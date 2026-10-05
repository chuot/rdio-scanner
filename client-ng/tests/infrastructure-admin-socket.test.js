import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { createAdminSocket } from '../src/infrastructure/admin-socket.js';

class FakeWebSocket {
    static instances = [];

    constructor(url) {
        this.url = url;
        this.readyState = 1;
        this.sent = [];
        this.onopen = null;
        this.onclose = null;
        this.onmessage = null;
        FakeWebSocket.instances.push(this);
    }

    send(data) {
        this.sent.push(data);
    }

    close(code = 1000) {
        this.readyState = 3;
        this.onclose?.({ code });
    }

    open() {
        this.onopen?.();
    }

    emit(data) {
        this.onmessage?.({ data });
    }
}

function setup(options = {}) {
    FakeWebSocket.instances = [];
    const scheduled = [];

    const events = [];
    const socket = createAdminSocket({
        url: 'ws://localhost:3000/api/admin/config',
        token: 'jwt-1',
        WebSocketImpl: FakeWebSocket,
        setTimeoutImpl: (callback, delay) => scheduled.push({ callback, delay }),
        ...options,
    });

    socket.subscribe((event) => events.push(event));

    return { socket, events, scheduled };
}

describe('infrastructure/admin-socket — handshake', () => {
    it('sends the raw JWT as the first text message on open', () => {
        const { socket } = setup();

        socket.connect();
        FakeWebSocket.instances[0].open();

        assert.deepEqual(FakeWebSocket.instances[0].sent, ['jwt-1']);
    });

    it('does not connect without a token', () => {
        const { socket } = setup({ token: '' });

        socket.connect();

        assert.equal(FakeWebSocket.instances.length, 0);
    });
});

describe('infrastructure/admin-socket — config push', () => {
    it('decodes pushed config objects', () => {
        const { socket, events } = setup();

        socket.connect();
        FakeWebSocket.instances[0].open();
        events.length = 0;

        FakeWebSocket.instances[0].emit(JSON.stringify({ systems: [{ id: 1 }] }));

        assert.deepEqual(events, [{ type: 'config', config: { systems: [{ id: 1 }] } }]);
    });
});

describe('infrastructure/admin-socket — close policy', () => {
    it('close code 1000 ends the session and does not reconnect', () => {
        const { socket, events, scheduled } = setup();

        socket.connect();
        FakeWebSocket.instances[0].open();
        events.length = 0;

        FakeWebSocket.instances[0].close(1000);

        assert.deepEqual(events, [{ type: 'unauthorized' }]);
        assert.equal(scheduled.length, 0);
    });

    it('any other close code reconnects after 2000 ms', () => {
        const { socket, scheduled } = setup();

        socket.connect();
        FakeWebSocket.instances[0].open();
        FakeWebSocket.instances[0].close(1006);

        assert.equal(scheduled.length, 1);
        assert.equal(scheduled[0].delay, 2000);

        scheduled[0].callback();

        assert.equal(FakeWebSocket.instances.length, 2);
    });

    it('close() detaches handlers so intentional logout does not reconnect', () => {
        const { socket, events, scheduled } = setup();

        socket.connect();
        FakeWebSocket.instances[0].open();
        socket.close();

        assert.equal(scheduled.length, 0);
        assert.equal(events.length, 0);
    });
});
