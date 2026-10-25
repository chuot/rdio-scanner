// tests for infrastructure/listener-socket: the open sequence
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { createListenerSocket } from '../infrastructure/listener-socket.js';

class FakeWebSocket {
    static instances = [];

    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSED = 3;

    constructor(url) {
        this.url = url;
        this.readyState = FakeWebSocket.CONNECTING;
        this.sent = [];
        this.onopen = null;
        this.onclose = null;
        this.onmessage = null;
        this.onerror = null;
        FakeWebSocket.instances.push(this);
    }

    send(data) {
        this.sent.push(JSON.parse(data));
    }

    close(code = 1000) {
        this.readyState = FakeWebSocket.CLOSED;
        this.onclose?.({ code });
    }

    open() {
        this.readyState = FakeWebSocket.OPEN;
        this.onopen?.();
    }

    emit(message) {
        this.onmessage?.({ data: JSON.stringify(message) });
    }
}

function createFakeTimer() {
    const scheduled = [];

    const setTimeoutImpl = (callback, delay) => {
        const entry = { callback, delay };
        scheduled.push(entry);
        return entry;
    };

    return {
        setTimeoutImpl,
        fireAll() {
        const pending = [...scheduled];
            scheduled.length = 0;
            pending.forEach((entry) => entry.callback());
        },
        get scheduled() {
            return scheduled;
        },
    };
}

function setup() {
    FakeWebSocket.instances = [];
    const timer = createFakeTimer();
    const events = [];

    const socket = createListenerSocket({
        url: 'ws://localhost:3000/',
        WebSocketImpl: FakeWebSocket,
        setTimeoutImpl: timer.setTimeoutImpl,
    });

    socket.subscribe((event) => events.push(event));

    return { socket, events, timer };
}

describe('infrastructure/listener-socket — open sequence', () => {
    it('connects to the page URL socket', () => {
        const { socket } = setup();

        socket.connect();

        assert.equal(FakeWebSocket.instances.length, 1);
        assert.equal(FakeWebSocket.instances[0].url, 'ws://localhost:3000/');
    });

    it('emits linked and sends VER then CFG on open', () => {
        const { socket, events } = setup();

        socket.connect();
        FakeWebSocket.instances[0].open();

        assert.deepEqual(events, [{ type: 'linked' }]);
        assert.deepEqual(FakeWebSocket.instances[0].sent, [['VER'], ['CFG']]);
    });
});

describe('infrastructure/listener-socket — message encoding', () => {
    it('encodes [command, payload, flag] and omits falsy payload and undefined flag', () => {
        const { socket } = setup();

        socket.connect();
        const ws = FakeWebSocket.instances[0];
        ws.open();
        ws.sent.length = 0;

        socket.send('CAL', '42', 'play');
        socket.send('CFG');
        socket.send('PIN', 'abc123');
        socket.send('LFM', null);

        assert.deepEqual(ws.sent, [
            ['CAL', '42', 'play'],
            ['CFG'],
            ['PIN', 'abc123'],
            ['LFM'],
        ]);
    });

    it('does not send while the socket is not open', () => {
        const { socket } = setup();

        socket.connect();
        socket.send('CFG');

        assert.equal(FakeWebSocket.instances[0].sent.length, 0);
    });
});

describe('infrastructure/listener-socket — message decoding', () => {
    it('decodes server frames into {type: message, command, payload, flag}', () => {
        const { socket, events } = setup();

        socket.connect();
        const ws = FakeWebSocket.instances[0];
        ws.open();
        events.length = 0;

        ws.emit(['CAL', { id: 7 }, 'play']);
        ws.emit(['LSC', 3]);
        ws.emit(['XPR']);

        assert.deepEqual(events, [
            { type: 'message', command: 'CAL', payload: { id: 7 }, flag: 'play' },
            { type: 'message', command: 'LSC', payload: 3, flag: undefined },
            { type: 'message', command: 'XPR', payload: undefined, flag: undefined },
        ]);
    });

    it('ignores non-array and invalid JSON frames without throwing', () => {
        const { socket, events } = setup();

        socket.connect();
        const ws = FakeWebSocket.instances[0];
        ws.open();
        events.length = 0;

        ws.onmessage({ data: 'not json' });
        ws.onmessage({ data: JSON.stringify({ command: 'CAL' }) });

        assert.deepEqual(events, []);
    });
});

describe('infrastructure/listener-socket — reconnect policy', () => {
    it('close code 1000 does not reconnect', () => {
        const { socket, timer } = setup();

        socket.connect();
        FakeWebSocket.instances[0].open();
        FakeWebSocket.instances[0].close(1000);

        assert.equal(timer.scheduled.length, 0);
        assert.equal(FakeWebSocket.instances.length, 1);
    });

    it('any other close code reconnects after 2000 ms', () => {
        const { socket, events, timer } = setup();

        socket.connect();
        FakeWebSocket.instances[0].open();
        FakeWebSocket.instances[0].close(1006);

        assert.deepEqual(events.at(-1), { type: 'unlinked' });
        assert.equal(timer.scheduled.length, 1);
        assert.equal(timer.scheduled[0].delay, 2000);

        timer.fireAll();

        assert.equal(FakeWebSocket.instances.length, 2);
        assert.equal(FakeWebSocket.instances[1].url, 'ws://localhost:3000/');
    });

    it('close() closes the socket without scheduling a reconnect', () => {
        const { socket, timer } = setup();

        socket.connect();
        FakeWebSocket.instances[0].open();
        socket.close();

        assert.equal(timer.scheduled.length, 0);
    });
});
