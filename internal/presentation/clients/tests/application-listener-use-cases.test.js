import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { createListenerUseCases } from '../src/application/listener-use-cases.js';
import { createStore } from '../shared/application/store.js';

function createFakeSocket() {
    const sent = [];
    const subscribers = new Set();

    return {
        sent,
        send: (command, payload, flag) => sent.push([command, payload, flag]),
        subscribe: (cb) => {
            subscribers.add(cb);
            return () => subscribers.delete(cb);
        },
        inject: (command, payload, flag) => {
            for (const cb of subscribers) cb({ type: 'message', command, payload, flag });
        },
        linked: () => {
            for (const cb of subscribers) cb({ type: 'linked' });
        },
    };
}

function createFakeStorage(initial = {}) {
    const map = new Map(Object.entries(initial));

    return {
        local: {
            getItem: (k) => (map.has(k) ? map.get(k) : null),
            setItem: (k, v) => map.set(k, String(v)),
            removeItem: (k) => map.delete(k),
            clear: () => map.clear(),
        },
        readLivefeedMap: (instanceId) => {
            const raw = map.get(`rdio-scanner-lfm-${instanceId}`);
            return raw ? JSON.parse(raw) : {};
        },
        writeLivefeedMap: (instanceId, encoded) => map.set(`rdio-scanner-lfm-${instanceId}`, JSON.stringify(encoded)),
        get stored() {
            return map;
        },
    };
}

function createFakeAudio() {
    const played = [];

    return {
        played,
        playCall: (call, handlers) => {
            played.push(call);
            call.ended = handlers.onEnded;
            return { stop: () => (call.stopped = true) };
        },
        playOscillators: async () => {},
        download: () => {},
    };
}

function createFakeTimers() {
    const scheduled = [];

    return {
        scheduled,
        setTimeoutImpl: (cb, delay) => {
            const entry = { cb, delay };
            scheduled.push(entry);
            return entry;
        },
        clearTimeoutImpl: (entry) => scheduled.splice(scheduled.indexOf(entry), 1),
        fireAll: () => {
            const pending = [...scheduled];
            scheduled.length = 0;
            pending.forEach((e) => e.cb());
        },
    };
}

const cfgPayload = {
    systems: [
        { id: 10, label: 'SYS1', talkgroups: [{ id: 100, label: 'TG1', groups: ['Fire'], tag: 'T1' }, { id: 101, label: 'TG2', groups: ['Fire'], tag: 'T1' }] },
        { id: 20, label: 'SYS2', talkgroups: [{ id: 200, label: 'TG3', groups: ['Police'], tag: 'T1' }] },
    ],
    groups: { Fire: { 10: [100, 101] }, Police: { 20: [200] } },
    groupsData: [{ id: 1, label: 'Fire' }],
    tags: { T1: { 10: [100, 101], 20: [200] } },
    tagsData: [{ id: 2, label: 'T1' }],
};

function setup(options = {}) {
    const socket = createFakeSocket();
    const storage = createFakeStorage(options.storage);
    const audio = createFakeAudio();
    const timers = createFakeTimers();
    const store = createStore({});
    const events = [];

    store.subscribe(['config', 'map', 'categories', 'call', 'queue', 'livefeedMode', 'holdSys', 'holdTg', 'auth', 'playbackList', 'pause', 'linked', 'listeners'], (e) => events.push(e));

    const useCases = createListenerUseCases({
        socket,
        storage,
        audio,
        store,
        instanceId: 'default',
        ...timers,
    });

    return { useCases, socket, storage, audio, timers, store, events };
}

describe('application/listener-use-cases — CFG boot sequence', () => {
    it('CFG rebuilds the livefeed map from storage; listener stays offline until started', () => {
        const { useCases, socket, storage } = setup({ storage: { 'rdio-scanner-lfm-default': JSON.stringify({ 10: { 100: false } }) } });

        socket.inject('CFG', cfgPayload);

        assert.equal(useCases.livefeedMode, 'offline');
        assert.equal(socket.sent.length, 0);
        assert.equal(useCases.livefeedMap[10][100].active, false);
        assert.equal(useCases.livefeedMap[20][200].active, true);
    });

    it('livefeed() goes online and sends the encoded map', () => {
        const { useCases, socket } = setup({ storage: { 'rdio-scanner-lfm-default': JSON.stringify({ 10: { 100: false } }) } });

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();

        assert.equal(useCases.livefeedMode, 'online');
        assert.deepEqual(socket.sent.at(-1), ['LFM', { 10: { 100: false, 101: true }, 20: { 200: true } }, undefined]);
    });

    it('CFG derives categories from the groups map', () => {
        const { useCases, socket } = setup();

        socket.inject('CFG', cfgPayload);

        assert.deepEqual(useCases.categories.map((c) => [c.label, c.status]), [['Fire', 'on'], ['Police', 'on']]);
    });
});

describe('application/listener-use-cases — call queueing and playback', () => {
    it('CAL enqueues and starts playback when idle', () => {
        const { useCases, socket, audio } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });

        assert.deepEqual(audio.played.map((c) => c.id), [1]);
        assert.equal(useCases.call?.id, 1);
        assert.equal(useCases.call.systemData.label, 'SYS1');
    });

    it('CAL while playing only queues', () => {
        const { useCases, socket, audio } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });
        socket.inject('CAL', { id: 2, system: 10, talkgroup: 101, audio: { data: [2] } });

        assert.deepEqual(audio.played.map((c) => c.id), [1]);
        assert.equal(useCases.callQueue.length, 1);
    });

    it('calls are ignored while offline', () => {
        const { useCases, socket, audio } = setup();

        socket.inject('CFG', cfgPayload);
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });

        assert.deepEqual(audio.played, []);
        assert.equal(useCases.callQueue.length, 0);
    });

    it('call end schedules the next play after the 1 s gap', () => {
        const { useCases, socket, audio, timers } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });
        socket.inject('CAL', { id: 2, system: 10, talkgroup: 101, audio: { data: [2] } });

        audio.played[0].ended();

        assert.equal(timers.scheduled.at(-1).delay, 1000);

        timers.fireAll();

        assert.deepEqual(audio.played.map((c) => c.id), [1, 2]);
    });

    it('calls for avoided talkgroups are filtered out of the queue', () => {
        const { useCases, socket, audio } = setup({ storage: { 'rdio-scanner-lfm-default': JSON.stringify({ 10: { 101: false } }) } });

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 101, audio: { data: [1] } });

        assert.deepEqual(audio.played, []);
        assert.equal(useCases.callQueue.length, 0);
    });
});

describe('application/listener-use-cases — avoid / hold / category', () => {
    it('avoid() walks the cycle and persists the map to storage and socket', () => {
        const { useCases, socket, storage } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();

        useCases.avoid({ system: 10, talkgroup: 100 });
        assert.equal(useCases.livefeedMap[10][100].active, false);
        assert.deepEqual(JSON.parse(storage.stored.get('rdio-scanner-lfm-default')), { 10: { 100: false, 101: true }, 20: { 200: true } });

        useCases.avoid({ system: 10, talkgroup: 100 });
        assert.equal(useCases.livefeedMap[10][100].minutes, 30);

        useCases.avoid({ system: 10, talkgroup: 100 });
        assert.equal(useCases.livefeedMap[10][100].minutes, 60);

        useCases.avoid({ system: 10, talkgroup: 100 });
        assert.equal(useCases.livefeedMap[10][100].minutes, 120);

        useCases.avoid({ system: 10, talkgroup: 100 });
        assert.equal(useCases.livefeedMap[10][100].active, true);
    });

    it('holdSystem snapshots the prior map, holds, and restores on second press', () => {
        const { useCases, socket } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();

        useCases.holdSystem({ system: 10, talkgroup: 100 });

        assert.equal(useCases.holdSys, true);
        assert.equal(useCases.livefeedMap[20][200].active, false);

        useCases.holdSystem({ system: 10, talkgroup: 100 });

        assert.equal(useCases.holdSys, false);
        assert.equal(useCases.livefeedMap[20][200].active, true);
    });

    it('holdSystem()/holdTalkgroup() with no argument fall back to callPrevious after the call ended', () => {
        const { useCases, socket, audio, timers } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });

        // the played call ends: onEnded -> skip({ delay: true }) -> gap timer
        audio.played[0].ended();
        timers.fireAll();

        assert.equal(useCases.call, null);
        assert.equal(useCases.callPrevious?.id, 1);

        useCases.holdSystem();

        assert.equal(useCases.holdSys, true);
        assert.equal(useCases.livefeedMap[20][200].active, false);

        useCases.holdSystem();

        assert.equal(useCases.holdSys, false);
        assert.equal(useCases.livefeedMap[20][200].active, true);

        useCases.holdTalkgroup();

        assert.equal(useCases.holdTg, true);
        assert.equal(useCases.livefeedMap[10][101].active, false);
        assert.equal(useCases.livefeedMap[10][100].active, true);
    });

    it('toggleCategory updates all member talkgroups and re-derives categories', () => {
        const { useCases, socket } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();

        useCases.toggleCategory({ label: 'Fire', status: 'on', type: 'group' });

        assert.equal(useCases.livefeedMap[10][100].active, false);
        assert.equal(useCases.livefeedMap[10][101].active, false);
        assert.deepEqual(useCases.categories.find((c) => c.label === 'Fire').status, 'off');
    });

    it('livefeed() toggle goes offline: clears queue and sends LFM null', () => {
        const { useCases, socket, audio } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });

        useCases.livefeed();

        assert.equal(useCases.livefeedMode, 'offline');
        assert.deepEqual(socket.sent.at(-1), ['LFM', null, undefined]);
        assert.equal(useCases.callQueue.length, 0);
    });

    it('avoid() returns the new avoided state so the controls pick the beep style', () => {
        const { useCases, socket } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();

        assert.equal(useCases.avoid({ system: 10, talkgroup: 100 }), true);
        assert.equal(useCases.avoid({ system: 10, talkgroup: 100 }), true);
        assert.equal(useCases.avoid({ system: 10, talkgroup: 100 }), true);
        assert.equal(useCases.avoid({ system: 10, talkgroup: 100 }), true);
        assert.equal(useCases.avoid({ system: 10, talkgroup: 100 }), false);

        assert.equal(useCases.avoid(), false);
    });

    it('setSystemStatus turns every talkgroup of one system on or off', () => {
        const { useCases, socket } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();

        useCases.setSystemStatus(10, false);

        assert.equal(useCases.livefeedMap[10][100].active, false);
        assert.equal(useCases.livefeedMap[10][101].active, false);
        assert.equal(useCases.livefeedMap[20][200].active, true);
        assert.deepEqual(useCases.categories.find((c) => c.label === 'Fire').status, 'off');

        useCases.setSystemStatus(10, true);

        assert.equal(useCases.livefeedMap[10][100].active, true);
        assert.equal(useCases.livefeedMap[10][101].active, true);
        assert.deepEqual(socket.sent.at(-1), ['LFM', { 10: { 100: true, 101: true }, 20: { 200: true } }, undefined]);
    });
});

describe('application/listener-use-cases — playback mode (search PLAY)', () => {
    it('loadAndPlay switches to playback mode and requests the call with play flag', () => {
        const { useCases, socket } = setup();

        socket.inject('CFG', cfgPayload);

        useCases.loadAndPlay(42);

        assert.equal(useCases.livefeedMode, 'playback');
        assert.deepEqual(socket.sent.at(-1), ['CAL', '42', 'play']);
    });

    it('the CAL reply for the pending id is queued with priority', () => {
        const { useCases, socket, audio } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.loadAndPlay(42);

        socket.inject('CAL', { id: 42, system: 10, talkgroup: 100, audio: { data: [1] } }, 'play');

        assert.equal(useCases.playbackPending, undefined);
        assert.deepEqual(audio.played.map((c) => c.id), [42]);
    });

    it('LCL responses become the transformed playback list', () => {
        const { useCases, socket } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.searchCalls({ limit: 200, offset: 0, sort: -1 });

        assert.deepEqual(socket.sent.at(-1), ['LCL', { limit: 200, offset: 0, sort: -1 }, undefined]);

        socket.inject('LCL', { count: 1, options: { limit: 200, offset: 0, sort: -1 }, results: [{ id: 5, system: 10, talkgroup: 100 }] });

        assert.equal(useCases.playbackList.results[0].systemData.label, 'SYS1');
    });
});

describe('application/listener-use-cases — auth', () => {
    it('authenticate sends the base64 pin; PIN reply demands a pin; MAX caps attempts', () => {
        const { useCases, socket, events } = setup();

        socket.inject('PIN');
        assert.equal(useCases.auth.status, 'pin-required');

        useCases.authenticate('secret');

        assert.deepEqual(socket.sent.at(-1), ['PIN', btoa('secret'), undefined]);
        assert.equal(useCases.auth.status, 'authenticating');

        socket.inject('PIN');
        assert.equal(useCases.auth.status, 'pin-required');
        assert.equal(useCases.auth.denied, true);

        socket.inject('MAX');
        assert.equal(useCases.auth.status, 'too-many');

        socket.inject('XPR');
        assert.equal(useCases.auth.status, 'expired');
    });

    it('stored pin auto-authenticates on link', () => {
        const { useCases, socket, storage } = setup({ storage: { 'rdio-scanner-pin': btoa('saved') } });

        socket.linked();

        assert.deepEqual(socket.sent.at(-1), ['PIN', btoa('saved'), undefined]);
    });

    it('LSC updates the listeners count', () => {
        const { useCases, socket } = setup();

        socket.inject('LSC', 7);

        assert.equal(useCases.listeners, 7);
    });
});
