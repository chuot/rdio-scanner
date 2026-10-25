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
    const suspends = [];
    const resumes = [];

    return {
        played,
        suspends,
        resumes,
        playCall: (call, handlers) => {
            played.push(call);
            call.ended = handlers.onEnded;
            return {
                stop: () => {
                    call.stopped = true;
                    // Mirror the engine's contract: a silenced recording never reaches its
                    // ended callback.
                    handlers.onEnded = null;
                },
            };
        },
        playOscillators: async () => {},
        suspend: () => suspends.push(true),
        resume: () => resumes.push(true),
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

// the select panel's talkgroup press is a plain flip: it switches the
// talkgroup on or off and never arms a timed avoid. a flip on a
// talkgroup the AVOID control left in a timed window clears the window
// and cancels its expiry.
describe('application/listener-use-cases — the select panel flip', () => {
    it('toggleTalkgroup flips an active talkgroup off with no minutes and no armed timer', () => {
        const { useCases, socket, timers } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();

        useCases.toggleTalkgroup(10, 100);

        assert.equal(useCases.livefeedMap[10][100].active, false);
        assert.equal(useCases.livefeedMap[10][100].minutes, undefined, 'the panel flip left a timed avoid on the entry the AVOID control alone may arm');
        assert.equal(timers.scheduled.length, 0, 'the panel flip scheduled a countdown');

        useCases.toggleTalkgroup(10, 100);

        assert.equal(useCases.livefeedMap[10][100].active, true, 'the second flip did not bring the talkgroup back on');
        assert.equal(timers.scheduled.length, 0, 'the flip back on armed a countdown');
    });

    it('toggleTalkgroup clears a timed avoid armed by avoid() and cancels its expiry', () => {
        const { useCases, socket, timers, events } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();

        useCases.avoid({ system: 10, talkgroup: 100 });
        useCases.avoid({ system: 10, talkgroup: 100 });

        assert.equal(useCases.livefeedMap[10][100].minutes, 30, 'the timed avoid was not armed as expected');
        assert.equal(timers.scheduled.length, 1, 'the timed avoid armed no expiry timer');

        useCases.toggleTalkgroup(10, 100);

        assert.equal(useCases.livefeedMap[10][100].active, true, 'the panel flip did not switch the timed-avoided talkgroup back on');
        assert.equal(useCases.livefeedMap[10][100].minutes, undefined, 'the flip left the timed window on the entry');
        assert.equal(timers.scheduled.length, 0, 'the flip left the expiry countdown armed');

        const emitted = events.length;
        timers.fireAll();

        assert.equal(events.length, emitted, 'the cancelled expiry still fired and re-announced the map');
    });

    it('toggleTalkgroup persists the flipped map and sends it like the other selection presses', () => {
        const { useCases, socket, storage, events } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();

        useCases.toggleTalkgroup(10, 100);

        assert.deepEqual(JSON.parse(storage.stored.get('rdio-scanner-lfm-default')), { 10: { 100: false, 101: true }, 20: { 200: true } });
        assert.deepEqual(socket.sent.at(-1), ['LFM', { 10: { 100: false, 101: true }, 20: { 200: true } }, undefined]);

        const last = events.at(-1);
        assert.ok('map' in last && 'categories' in last, 'the flip emitted no map and categories to the store');
        assert.equal(last.map[10][100].active, false);
    });

    it('toggleTalkgroup drops a queued call on the talkgroup the press switches off', () => {
        const { useCases, socket } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });
        socket.inject('CAL', { id: 2, system: 10, talkgroup: 101, audio: { data: [2] } });

        assert.equal(useCases.callQueue.length, 1, 'the second call was not queued as expected');

        useCases.toggleTalkgroup(10, 101);

        assert.equal(useCases.callQueue.length, 0, 'the queued call on the switched-off talkgroup stayed queued');
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

// the queue count on a searched playback is the calls that will play
// from the one being played up to the newest of the searched history,
// the played call among them. playback walks toward the newest call in
// both sort orders, so every SKIP NEXT takes one off the number.
describe('application/listener-use-cases — the queue count steps down on every skip', () => {
    // walks a searched playback from a row of the answered page, `steps`
    // skips deep. the call each skip asks for is read from the request the
    // use case sent, so the walk itself is what the test sees.
    function walkThrough({ sort, page, from, steps, offset = 0, count = page.length, limit = 200 }) {
        const { useCases, socket, store } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.loadAndPlay(page[from]);

        socket.inject('LCL', {
            count,
            options: { limit, offset, sort },
            results: page.map((id) => ({ id, system: 10, talkgroup: 100 })),
        });

        socket.inject('CAL', { id: page[from], system: 10, talkgroup: 100, audio: { data: [0] } }, 'play');

        const counts = [store.getState().queue];
        const played = [page[from]];

        for (let i = 0; i < steps; i += 1) {
            useCases.skip();

            const asked = Number(socket.sent.at(-1)[1]);
            played.push(asked);

            socket.inject('CAL', { id: asked, system: 10, talkgroup: 100, audio: { data: [i + 1] } }, 'play');
            counts.push(store.getState().queue);
        }

        return { counts, played };
    }

    it('a descending playback walks to the newer calls and the queue count steps down at every skip', () => {
        // The page as the operator sees it: newest call in the first row, oldest in the last.
        const { counts, played } = walkThrough({ sort: -1, page: [4, 3, 2, 1], from: 3, steps: 3 });

        assert.deepEqual(played, [1, 2, 3, 4], 'each skip plays the call newer than the one it stopped');
        assert.deepEqual(counts, [4, 3, 2, 1], 'the calls left to play, the one being played among them');
    });

    it('an ascending playback walks to the newer calls and the queue count steps down at every skip', () => {
        const { counts, played } = walkThrough({ sort: 1, page: [1, 2, 3, 4], from: 0, steps: 3 });

        assert.deepEqual(played, [1, 2, 3, 4], 'each skip plays the call newer than the one it stopped');
        assert.deepEqual(counts, [4, 3, 2, 1], 'the same number the descending page reads, meaning the same thing');
    });

    it('the queue count reads the filters the operator applied, not the whole archive', () => {
        // The operator narrowed the history to the four calls of one talkgroup and clicked its
        // second row. The archive holds far more calls than the filter leaves.
        const { counts, played } = walkThrough({ sort: 1, page: [3, 6, 9, 12], from: 1, steps: 2, count: 4 });

        assert.deepEqual(played, [6, 9, 12], 'the walk plays the calls the filter left, in the order the list shows them');
        assert.deepEqual(
            counts,
            [3, 2, 1],
            'the calls the filter matched, the one playing among them — not the calls the archive holds',
        );
    });

    it('the queue count reads the page of the filtered search the operator is on', () => {
        const { counts } = walkThrough({ sort: -1, page: [4, 3, 2, 1], from: 1, steps: 1, offset: 200, count: 250 });

        assert.deepEqual(counts, [202, 201], 'the calls ahead of the row the operator clicked, across the pages of the filtered search');
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

// the searched history is longer than the page the server answered:
// the walk toward the newest call runs off the page, and the calls it
// needs are on the page beside it, the lower offset for descending,
// the higher for ascending. with no newer page left, the walk ends.
describe('application/listener-use-cases — the searched playback crosses to the page of newer calls', () => {
    function playbackOn({ sort, page, from, offset = 0, count = page.length, limit = 200 }) {
        const { useCases, socket, store } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.loadAndPlay(page[from]);

        socket.inject('LCL', {
            count,
            options: { limit, offset, sort },
            results: page.map((id) => ({ id, system: 10, talkgroup: 100 })),
        });

        socket.inject('CAL', { id: page[from], system: 10, talkgroup: 100, audio: { data: [0] } }, 'play');

        return { useCases, socket, store };
    }

    const as = (ids) => ids.map((id) => ({ id, system: 10, talkgroup: 100 }));

    it('a descending playback at the newest row of its page asks for the page of newer calls', () => {
        const { useCases, socket } = playbackOn({ sort: -1, page: [4, 3, 2, 1], from: 0, offset: 200, count: 400 });

        useCases.skip();

        assert.deepEqual(
            socket.sent.at(-1),
            ['LCL', { limit: 200, offset: 0, sort: -1 }, undefined],
            'the newer calls sit at the lower offset of a descending history',
        );

        socket.inject('LCL', { count: 400, options: { limit: 200, offset: 0, sort: -1 }, results: as([8, 7, 6, 5]) });

        assert.deepEqual(
            socket.sent.at(-1),
            ['CAL', '5', 'play'],
            'the walk resumes at the oldest call of the newer page, the call newer than the one it stopped',
        );
    });

    it('an ascending playback past the last row of its page asks for the page of newer calls', () => {
        const { useCases, socket } = playbackOn({ sort: 1, page: [1, 2, 3, 4], from: 3, offset: 0, count: 250 });

        useCases.skip();

        assert.deepEqual(
            socket.sent.at(-1),
            ['LCL', { limit: 200, offset: 200, sort: 1 }, undefined],
            'the newer calls sit at the higher offset of an ascending history',
        );

        socket.inject('LCL', { count: 250, options: { limit: 200, offset: 200, sort: 1 }, results: as([5, 6, 7, 8]) });

        assert.deepEqual(
            socket.sent.at(-1),
            ['CAL', '5', 'play'],
            'the walk resumes at the first call of the newer page',
        );
    });

    it('a playback with no newer calls left stops the searched playback and returns to the live feed', () => {
        const { useCases, socket, store } = playbackOn({ sort: -1, page: [4, 3, 2, 1], from: 0, offset: 0, count: 4 });

        useCases.skip();

        assert.deepEqual(
            socket.sent.at(-1),
            ['LCL', { limit: 200, offset: 0, sort: -1 }, undefined],
            'the first ask re-reads the page it is on',
        );

        socket.inject('LCL', { count: 4, options: { limit: 200, offset: 0, sort: -1 }, results: as([4, 3, 2, 1]) });

        assert.equal(useCases.livefeedMode, 'online', 'the answer carries no newer call, so the listener goes live');
        assert.equal(store.getState().queue, 0, 'and the number says nothing waits');
    });
});

// the search panel's progress bar clears on its store subscription to
// the playback list, and the store deep-compares slices, so an answer
// equal to the list on screen notifies nobody and the bar runs forever:
// so every call-list answer carries a sequence counter that cannot match.
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

// the alert tone in front of a live call. the fake engine keeps the
// tone sequences and recordings it is asked for, in order, and holds
// each tone open until the test ends it. the tone is a live arrival's:
// replayed, searched, and playback-mode calls start with no tone.

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

    // the queue is the number the operator watches while the tone is open:
    // a call taken from the queue has left it, and one arriving during the
    // tone is in it. the number carried when the recording starts is the
    // queue as it stands then, not the one captured before the tone.
    it('a skip behind an open tone decrements the queue count before the tone ends', async () => {
        const audio = createToneAwareAudio();
        const { useCases, socket, store } = setup({ audio });

        socket.inject('CFG', talkgroupToneCfg);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });
        audio.endTone();
        await settle();

        socket.inject('CAL', { id: 2, system: 10, talkgroup: 100, audio: { data: [2] } });
        socket.inject('CAL', { id: 3, system: 10, talkgroup: 101, audio: { data: [3] } });

        assert.equal(store.getState().queue, 2, 'two calls wait behind the one being played');

        useCases.skip();
        await settle();

        assert.equal(store.getState().queue, 1, 'the call taken out for its tone has left the queue');

        audio.endTone();
        await settle();

        assert.equal(store.getState().queue, 1, 'and the tone end carries that same live number');
        assert.equal(store.getState().call?.id, 2);
    });

    it('a call arriving while a tone is open is counted in the number the tone end carries', async () => {
        const audio = createToneAwareAudio();
        const { useCases, socket, store } = setup({ audio });

        socket.inject('CFG', talkgroupToneCfg);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });
        audio.endTone();
        await settle();

        socket.inject('CAL', { id: 2, system: 10, talkgroup: 100, audio: { data: [2] } });

        useCases.skip();
        await settle();

        assert.equal(store.getState().queue, 0, 'the queue is empty while the tone is open, the call having left it');

        socket.inject('CAL', { id: 3, system: 10, talkgroup: 101, audio: { data: [3] } });

        assert.equal(store.getState().queue, 1, 'the arrival joins the queue while the tone is open');

        audio.endTone();
        await settle();

        assert.equal(store.getState().queue, 1, 'the tone end counts it instead of overwriting it with the pre-tone number');
        assert.equal(store.getState().call?.id, 2);
    });
});

// every action that ends a call silences the recording in flight: the
// use case keeps the engine's handle and uses it in its stop funnel.
// a silenced recording must never trigger its follow-on skip: a browser
// fires `ended` on a manual stop too, so the ended closure checks its play.
describe('application/listener-use-cases — every action that ends a call silences the recording', () => {
    function playingCall() {
        const harness = setup();
        const { useCases, socket } = harness;

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });

        return harness;
    }

    it('SKIP NEXT stops the recording it skips past', async () => {
        const { useCases, socket, audio } = playingCall();
        await settle();

        socket.inject('CAL', { id: 2, system: 10, talkgroup: 100, audio: { data: [2] } });

        useCases.skip();

        assert.equal(audio.played[0].stopped, true, 'the skipped recording is silenced');
    });

    it('LIVE FEED off stops the recording in flight', async () => {
        const { useCases, audio } = playingCall();
        await settle();

        useCases.livefeed();

        assert.equal(audio.played[0].stopped, true, 'turning the livefeed off silences the call it was playing');
    });

    it('a search-row play-instead silences the call it replaces, and ending playback mode silences its own', async () => {
        const { useCases, socket, audio } = playingCall();
        await settle();

        useCases.loadAndPlay(7);
        socket.inject('CAL', { id: 7, system: 10, talkgroup: 100, audio: { data: [7] } }, 'play');

        assert.equal(audio.played[0].stopped, true, 'the live call the search replaced is silenced');
        assert.equal(audio.played[1].stopped, undefined, 'the replacement plays on');

        useCases.stopPlaybackMode();

        assert.equal(audio.played[1].stopped, true, 'ending playback mode silences the call it retires');
    });

    it('a silenced recording fires no follow-on skip', async () => {
        const { useCases, socket, audio, timers } = playingCall();
        await settle();

        socket.inject('CAL', { id: 2, system: 10, talkgroup: 100, audio: { data: [2] } });
        socket.inject('CAL', { id: 3, system: 10, talkgroup: 100, audio: { data: [3] } });

        useCases.skip();
        await settle();

        // The browser still fires `ended` on the stopped recording as it winds down.
        audio.played[0].ended();
        timers.fireAll();
        await settle();

        assert.deepEqual(audio.played.map((c) => c.id), [1, 2], 'the silenced recording advances the queue no further');
    });
});

// the pause reaches the audio port: suspending the playback context
// silences the recording in flight and the re-arm gate keeps it
// silenced; unpausing resumes the context and continues or starts.
describe('application/listener-use-cases — pause reaches the audio port', () => {
    it('PAUSE suspends the playback context; unpausing resumes it and the recording continues', async () => {
        const { useCases, socket, audio, timers } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });
        await settle();
        socket.inject('CAL', { id: 2, system: 10, talkgroup: 100, audio: { data: [2] } });

        useCases.pause(true);

        assert.deepEqual(audio.suspends, [true], 'the port is told to suspend the call audio');
        assert.deepEqual(audio.resumes, []);

        useCases.pause(false);

        assert.deepEqual(audio.resumes, [true], 'the port is told to resume');
        assert.deepEqual(audio.played.map((c) => c.id), [1], 'the paused recording resumes where it stopped, it is not replaced');

        // The recording in flight ends naturally; only then does the queued call take its turn.
        audio.played[0].ended();
        timers.fireAll();
        await settle();

        assert.deepEqual(audio.played.map((c) => c.id), [1, 2], 'the queued call plays when the recording ends');
    });

    it('a call that arrives during a pause waits for the unpause', async () => {
        const { useCases, socket, audio } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();
        useCases.pause(true);

        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [1] } });

        assert.deepEqual(audio.played.map((c) => c.id), [], 'nothing plays while paused');

        useCases.pause(false);
        await settle();

        assert.deepEqual(audio.played.map((c) => c.id), [1], 'the unpause plays it');
    });
});

// the owner's report: in archive playback mode, PAUSE then SKIP NEXT
// read the queue as 0 though the walk was full. while nothing plays,
// the gap a paused skip opens counts from the call the walk last played.
// live mode's number is the queue length and never takes this path.
describe('application/listener-use-cases — the queue number stays truthful across a paused skip', () => {
    it('a paused playback skip keeps the walk count and never reads zero', async () => {
        const { useCases, socket, store } = setup();

        socket.inject('CFG', cfgPayload);

        const page = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11];
        useCases.loadAndPlay(1);
        socket.inject('LCL', {
            count: 11,
            options: { limit: 200, offset: 0, sort: 1 },
            results: page.map((id) => ({ id, system: 10, talkgroup: 100 })),
        });
        socket.inject('CAL', { id: 1, system: 10, talkgroup: 100, audio: { data: [0] } }, 'play');

        assert.equal(store.getState().queue, 11, 'the calls left to play, the one playing among them');

        useCases.pause(true);
        assert.equal(store.getState().queue, 11, 'the pause keeps the number');

        useCases.skip();

        // The call the skip asked for arrives into the gap the paused skip opened.
        socket.inject('CAL', { id: 2, system: 10, talkgroup: 100, audio: { data: [1] } }, 'play');

        assert.equal(store.getState().queue, 11, 'the number counts from the call the walk last played, not from a call that is not playing');

        useCases.pause(false);

        assert.equal(useCases.call?.id, 2, 'the skipped-to call starts');
        assert.equal(store.getState().queue, 10, 'the number steps down by exactly the one call the skip took');
    });

    it('live mode keeps its queue length across a paused skip', async () => {
        const { useCases, socket, store } = setup();

        socket.inject('CFG', cfgPayload);
        useCases.livefeed();

        for (let id = 1; id <= 11; id += 1) socket.inject('CAL', { id, system: 10, talkgroup: 100, audio: { data: [id] } });
        await settle();

        assert.equal(store.getState().queue, 10, 'ten waiting while one plays');

        useCases.pause(true);
        useCases.skip();
        socket.inject('CAL', { id: 12, system: 10, talkgroup: 100, audio: { data: [12] } });

        assert.equal(store.getState().queue, 11, 'the arrival counts; the number never zeroes');

        useCases.pause(false);
        await settle();

        assert.equal(store.getState().queue, 10, 'the resumed call left the queue');
    });
});
