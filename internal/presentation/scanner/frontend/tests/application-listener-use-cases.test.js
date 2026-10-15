import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { createListenerUseCases } from '../application/listener-use-cases.js';
import { createStore } from '../../../common/frontend/application/store.js';

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
    const audio = options.audio ?? createFakeAudio();
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

// The queue count on a searched playback is the number of calls waiting to play after the one
// being played. SKIP NEXT stops the current call and starts the next queued one, so each skip
// hands one call out of the queue and the count steps down. The count must never climb on a
// skip, whichever way the page is sorted: playback walks forward through the page in both orders.
describe('application/listener-use-cases — the queue count steps down on every skip', () => {
    function playback(options) {
        const { useCases, socket, store } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.loadAndPlay(options.ids[0]);

        socket.inject('LCL', {
            count: options.ids.length,
            options: { limit: 200, offset: 0, sort: options.sort },
            results: options.ids.map((id) => ({ id, system: 10, talkgroup: 100 })),
        });

        return { useCases, socket, store };
    }

    function skipThrough(options) {
        const { useCases, socket, store } = playback(options);

        socket.inject('CAL', { id: options.ids[0], system: 10, talkgroup: 100, audio: { data: [0] } }, 'play');
        const counts = [store.getState().queue];

        for (let i = 1; i < options.ids.length; i += 1) {
            useCases.skip();
            socket.inject('CAL', { id: options.ids[i], system: 10, talkgroup: 100, audio: { data: [i] } }, 'play');
            counts.push(store.getState().queue);
        }

        return counts;
    }

    it('three skips on a descending playback step the queue count down, never up', () => {
        const counts = skipThrough({ sort: -1, ids: [1, 2, 3, 4] });

        assert.deepEqual(counts, [3, 2, 1, 0]);
    });

    it('three skips on an ascending playback step the queue count down, never up', () => {
        const counts = skipThrough({ sort: 1, ids: [1, 2, 3, 4] });

        assert.deepEqual(counts, [3, 2, 1, 0]);
    });

    it('the live feed queue counts the calls waiting: arrivals add, playing a queued call subtracts', () => {
        const { useCases, socket, store } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();

        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });
        assert.equal(store.getState().queue, 0, 'the first call plays at once and nothing waits behind it');

        socket.inject('CAL', { id: 2, system: 10, talkgroup: 101, audio: { data: [2] } });
        socket.inject('CAL', { id: 3, system: 10, talkgroup: 100, audio: { data: [3] } });
        assert.equal(store.getState().queue, 2, 'two arrivals wait behind the call being played');

        useCases.skip();
        assert.equal(store.getState().queue, 1, 'skipping hands the next call out of the queue');

        useCases.skip();
        assert.equal(store.getState().queue, 0, 'and the skip after it hands out the last one');
    });
});

// The search panel's progress bar is cleared by its store subscription on the playback list.
// The store deep-compares slices, so an answer equal to the list already on screen notifies
// nobody and the bar runs forever. Every call-list answer must carry a signal that cannot
// compare equal: a sequence counter.
describe('application/listener-use-cases — call-list answers always notify', () => {
    const page = { count: 0, options: { limit: 200, offset: 0, sort: -1 }, results: [] };

    function countNotices(options = {}) {
        const socket = createFakeSocket();
        const store = createStore({});
        let notices = 0;

        store.subscribe(['playbackList', 'playbackListSeq'], () => notices++);

        const useCases = createListenerUseCases({
            socket,
            storage: createFakeStorage(),
            audio: createFakeAudio(),
            store,
            instanceId: 'default',
            ...createFakeTimers(),
        });

        socket.inject('CFG', cfgPayload);

        for (const payload of options.answers) {
            socket.inject('LCL', payload);
        }

        return { useCases, notices };
    }

    it('two deep-equal answers both notify the playback-list subscriber', () => {
        const { notices } = countNotices({ answers: [page, page] });

        assert.equal(notices, 2, 'the second identical answer was swallowed by the store diff');
    });

    it('a zero-result answer notifies', () => {
        const { useCases, notices } = countNotices({ answers: [page] });

        assert.equal(notices, 1);
        assert.equal(useCases.playbackList.count, 0);
    });

    it('a null payload notifies with the playback list cleared', () => {
        const { useCases, notices } = countNotices({ answers: [page, null] });

        assert.equal(notices, 2, 'the null answer emitted nothing and the panel never learns the search ended');
        assert.equal(useCases.playbackList, null);
    });

    it('a payload without a results array clears the list and throws nothing', () => {
        const { useCases, notices } = countNotices({ answers: [{ count: 0 }, null] });

        assert.equal(notices, 2);
        assert.equal(useCases.playbackList, null);
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

// The alert tone in front of a live call. The fake engine below keeps, in the order it was asked,
// the tone sequences it was given and the recordings it was asked to start, and holds each tone open
// until the test ends it, the way the audio engine resolves a sequence when its last oscillator ends.
//
// The tone is a live arrival's: a call the listener replays, a call opened from the search panel, and
// anything in playback mode start their recording with no tone in front of it.

function createToneAwareAudio() {
    const schedule = [];
    const open = [];

    return {
        schedule,

        playCall: (call, handlers) => {
            schedule.push({ recording: call.id });

            call.ended = handlers.onEnded;

            return { stop: () => (call.stopped = true) };
        },

        playOscillators: (sequence) => {
            schedule.push({ tone: sequence });

            return new Promise((resolve) => open.push({ sequence, resolve }));
        },

        // endTone ends the oldest tone that is waiting, as the audio engine does when the last
        // oscillator of a sequence ends.
        endTone: () => {
            const tone = open.shift();

            assert.ok(tone, 'no tone is waiting to be ended');

            tone.resolve();

            return tone.sequence;
        },

        download: () => {},
    };
}

// The play path continues in a promise, so a test that ends a tone has to let the queued work run
// before it can see what the engine was asked to do next.
function settle() {
    return new Promise((done) => setImmediate(done));
}

const alert5 = [
    { begin: 0, end: 0.125, frequency: 800, type: 'square' },
    { begin: 0.375, end: 0.5, frequency: 800, type: 'square' },
    { begin: 1.125, end: 1.5, frequency: 1000, type: 'square' },
];

// The tone comes from the group Fire, which talkgroups 100 and 101 both belong to.
const groupToneCfg = {
    ...cfgPayload,
    alerts: { alert5 },
    groupsData: [{ id: 1, label: 'Fire', alert: 'alert5' }],
};

// The tone comes from talkgroup 100 alone, so a call on talkgroup 101 carries none.
const talkgroupToneCfg = {
    ...cfgPayload,
    alerts: { alert5 },
    systems: [
        {
            id: 10,
            label: 'SYS1',
            talkgroups: [
                { id: 100, label: 'TG1', groups: ['Fire'], tag: 'T1', alert: 'alert5' },
                { id: 101, label: 'TG2', groups: ['Fire'], tag: 'T1' },
            ],
        },
        { id: 20, label: 'SYS2', talkgroups: [{ id: 200, label: 'TG3', groups: ['Police'], tag: 'T1' }] },
    ],
};

describe('application/listener-use-cases — the alert tone in front of a live call', () => {
    it('a live call whose group carries a tone plays the tone, then starts its recording', async () => {
        const audio = createToneAwareAudio();
        const { useCases, socket, store } = setup({ audio });

        socket.inject('CFG', groupToneCfg);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });

        assert.deepEqual(audio.schedule, [{ tone: alert5 }], 'the tone is scheduled before any recording starts');
        assert.ok(!store.getState().call, 'the call reaches the store when its recording starts, not when its tone does');

        audio.endTone();
        await settle();

        assert.deepEqual(audio.schedule, [{ tone: alert5 }, { recording: 1 }], 'the recording starts after the tone ends');
        assert.equal(store.getState().call?.id, 1);
    });

    it('a live call with no alert starts its recording with no tone scheduled', async () => {
        const audio = createToneAwareAudio();
        const { useCases, socket } = setup({ audio });

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });
        await settle();

        assert.deepEqual(audio.schedule, [{ recording: 1 }]);
    });

    it('a call opened from the search panel in playback mode starts with no tone', async () => {
        const audio = createToneAwareAudio();
        const { useCases, socket } = setup({ audio });

        socket.inject('CFG', groupToneCfg);
        useCases.loadAndPlay(42);
        socket.inject('CAL', { id: 42, system: 10, talkgroup: 100, audio: { data: [1] } }, 'play');
        await settle();

        assert.equal(useCases.livefeedMode, 'playback');
        assert.deepEqual(audio.schedule, [{ recording: 42 }], 'a recording the listener asked for carries no tone');
    });

    it('the replay of the previous call plays no tone', async () => {
        const audio = createToneAwareAudio();
        const { useCases, socket, timers } = setup({ audio });

        socket.inject('CFG', groupToneCfg);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });

        audio.endTone();
        await settle();

        useCases.call.ended();
        timers.fireAll();

        assert.deepEqual(audio.schedule, [{ tone: alert5 }, { recording: 1 }]);

        useCases.replay();
        await settle();

        assert.deepEqual(
            audio.schedule.at(-1),
            { recording: 1 },
            'a replay of a call whose arrival carried a tone schedules no tone of its own',
        );
    });

    it('a call skipped while its tone is playing never starts its recording, and the queued call plays', async () => {
        const audio = createToneAwareAudio();
        const { useCases, socket } = setup({ audio });

        socket.inject('CFG', talkgroupToneCfg);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });
        socket.inject('CAL', { id: 2, system: 10, talkgroup: 101, audio: { data: [2] } });

        assert.deepEqual(audio.schedule, [{ tone: alert5 }]);

        useCases.skip();
        await settle();

        assert.deepEqual(audio.schedule, [{ tone: alert5 }, { recording: 2 }], 'the queue carries on while the tone is open');

        audio.endTone();
        await settle();

        assert.deepEqual(
            audio.schedule,
            [{ tone: alert5 }, { recording: 2 }],
            'the skipped call recording never starts after its tone ends',
        );
        assert.equal(useCases.call?.id, 2);
    });
});
