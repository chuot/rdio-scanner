// tests for infrastructure/audio-engine: dual contexts and the gesture bootstrap
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { PLAYBACK_GAP_MS, PROGRESS_TICK_MS, createAudioEngine } from '../infrastructure/audio-engine.js';

class FakeAudioContext {
    static instances = [];

    constructor(options) {
        this.options = options;
        this.state = 'suspended';
        this.resumeCalls = 0;
        this.currentTime = 0;
        this.onstatechange = null;
        this.decoded = [];
        this.sources = [];
        this.oscillators = [];
        this.destination = { id: 'destination' };
        FakeAudioContext.instances.push(this);
    }

    advance(seconds) {
        // A suspended context freezes its clock; time only moves while it runs.
        if (this.state === 'running') this.currentTime += seconds;
    }

    resume() {
        this.resumeCalls++;
        this.state = 'running';
        return Promise.resolve();
    }

    suspend() {
        this.suspendCalls = (this.suspendCalls ?? 0) + 1;
        this.state = 'suspended';
        // A real suspension fires a statechange, and that is exactly what an ungated
        // re-arm loop would seize on to undo the pause.
        this.onstatechange?.();
        return Promise.resolve();
    }

    decodeAudioData(arrayBuffer, onsuccess) {
        this.decoded.push(arrayBuffer);
        onsuccess({ duration: 2 });
    }

    createBufferSource() {
        const source = {
            buffer: null,
            connections: [],
            started: false,
            onended: null,
            connect: (target) => source.connections.push(target),
            start: () => (source.started = true),
            stop: () => {
                source.stopped = true;
                // Browsers fire `ended` on a manual stop too; a handle that wants the
                // callback silenced must disarm it before stopping.
                source.onended?.();
            },
            disconnect: () => (source.disconnected = true),
        };
        this.sources.push(source);
        return source;
    }

    createGain() {
        return { gain: { value: null }, connect: () => {} };
    }

    createOscillator() {
        const osc = { frequency: { value: null }, type: null, connect: () => {}, start: () => {}, stop: () => {}, onended: null };
        this.oscillators.push(osc);
        return osc;
    }
}

// The real decodeAudioData never answers synchronously: the success callback
// runs in a later task. This context holds the callback until the test hands it
// back, which is the only way a stop can overtake a decode.
class DeferredDecodeAudioContext extends FakeAudioContext {
    constructor(options) {
        super(options);
        this.pendingDecode = [];
    }

    decodeAudioData(arrayBuffer, onsuccess) {
        this.decoded.push(arrayBuffer);
        this.pendingDecode.push(onsuccess);
    }

    completeDecode() {
        const [onsuccess] = this.pendingDecode.splice(0, 1);
        onsuccess({ duration: 2 });
    }
}

function createFakeDocument() {
    const listeners = { keydown: [], mousedown: [], touchstart: [] };
    const appended = [];
    const created = [];

    return {
        listeners,
        appended,
        created,
        body: {
            addEventListener: (type, handler) => listeners[type].push(handler),
            removeEventListener: (type, handler) => {
                listeners[type] = listeners[type].filter((h) => h !== handler);
            },
            appendChild: (el) => appended.push(el),
            removeChild: (el) => appended.splice(appended.indexOf(el), 1),
        },
        createElement: (tag) => {
            const el = { tag, style: {}, attributes: {}, setAttribute(k, v) { this.attributes[k] = v; }, click() { this.clicked = true; } };
            created.push(el);
            return el;
        },
    };
}

function setup() {
    FakeAudioContext.instances = [];
    const documentImpl = createFakeDocument();

    const engine = createAudioEngine({ AudioContextImpl: FakeAudioContext, documentImpl });

    return { engine, documentImpl };
}

function setupDeferredDecode() {
    FakeAudioContext.instances = [];
    const documentImpl = createFakeDocument();

    const engine = createAudioEngine({ AudioContextImpl: DeferredDecodeAudioContext, documentImpl });

    return { engine, documentImpl };
}

describe('infrastructure/audio-engine — dual contexts and gesture bootstrap', () => {
    it('bootstrap attaches gesture listeners and creates playback + interactive contexts on first gesture', async () => {
        const { engine, documentImpl } = setup();

        engine.bootstrap();

        assert.equal(documentImpl.listeners.keydown.length, 1);
        assert.equal(FakeAudioContext.instances.length, 0);

        await documentImpl.listeners.keydown[0]();

        assert.deepEqual(FakeAudioContext.instances.map((ctx) => ctx.options.latencyHint), ['playback', 'interactive']);

        // both contexts exist → listeners removed
        assert.equal(documentImpl.listeners.keydown.length, 0);
        assert.equal(documentImpl.listeners.mousedown.length, 0);
        assert.equal(documentImpl.listeners.touchstart.length, 0);
    });

    it('resumes suspended contexts and re-arms the resume loop on statechange', async () => {
        const { engine, documentImpl } = setup();

        engine.bootstrap();
        await documentImpl.listeners.mousedown[0]();

        const [playback, interactive] = FakeAudioContext.instances;

        assert.ok(playback.resumeCalls >= 1);
        assert.ok(interactive.resumeCalls >= 1);
        assert.equal(typeof playback.onstatechange, 'function');

        playback.state = 'suspended';
        playback.onstatechange();
        await Promise.resolve();

        assert.equal(playback.state, 'running');
    });
});

describe('infrastructure/audio-engine — pause gates the playback context', () => {
    it('suspend suspends the playback context and the re-arm loop does not undo it', async () => {
        const { engine, documentImpl } = setup();

        engine.bootstrap();
        await documentImpl.listeners.mousedown[0]();

        const [playback, interactive] = FakeAudioContext.instances;

        engine.suspend();
        await Promise.resolve();

        assert.equal(playback.state, 'suspended', 'the port can suspend the call audio');
        assert.equal(interactive.state, 'running', 'keypad and alert tones keep playing during a pause');

        const resumeCalls = playback.resumeCalls;
        playback.onstatechange();
        await Promise.resolve();

        assert.equal(playback.state, 'suspended', 'the intentional pause survives the re-arm loop');
        assert.equal(playback.resumeCalls, resumeCalls, 'the loop never resumes a paused context');
    });

    it('resume re-arms the playback context', async () => {
        const { engine, documentImpl } = setup();

        engine.bootstrap();
        await documentImpl.listeners.mousedown[0]();

        const [playback] = FakeAudioContext.instances;

        engine.suspend();
        engine.resume();
        await Promise.resolve();

        assert.equal(playback.state, 'running');

        playback.state = 'suspended';
        playback.onstatechange();
        await Promise.resolve();

        assert.equal(playback.state, 'running', 'the loop re-arms once the pause is lifted');
    });

    it('suspend and resume are no-ops before bootstrap', () => {
        const { engine } = setup();

        engine.suspend();
        engine.resume();

        assert.equal(FakeAudioContext.instances.length, 0);
    });
});

describe('infrastructure/audio-engine — the recording clock', () => {
    it('elapsed advances with the context clock, freezes across a pause, and restarts on a new recording', async () => {
        const { engine, documentImpl } = setup();

        engine.bootstrap();
        await documentImpl.listeners.touchstart[0]();

        const [playback] = FakeAudioContext.instances;

        assert.equal(engine.playbackElapsed(), null, 'nothing is playing yet');

        const handle = engine.playCall({ id: 1, audio: { data: [1] } }, {});

        playback.advance(3);
        assert.equal(engine.playbackElapsed(), 3);

        engine.suspend();
        playback.advance(5);
        assert.equal(engine.playbackElapsed(), 3, 'a suspended context freezes the clock with the audio');

        engine.resume();
        playback.advance(2);
        assert.equal(engine.playbackElapsed(), 5);

        handle.stop();
        assert.equal(engine.playbackElapsed(), null, 'a stopped recording has no clock');

        engine.playCall({ id: 2, audio: { data: [2] } }, {});
        assert.equal(engine.playbackElapsed(), 0, 'a new recording restarts the elapsed count at zero');

        playback.advance(1);
        assert.equal(engine.playbackElapsed(), 1);

        playback.sources[1].onended();
        assert.equal(engine.playbackElapsed(), null, 'a natural end clears the clock');
    });

    it('elapsed is null before bootstrap', () => {
        const { engine } = setup();

        assert.equal(engine.playbackElapsed(), null);
    });
});

describe('infrastructure/audio-engine — call playback', () => {
    it('decodes the call audio bytes and plays through a buffer source', async () => {
        const { engine, documentImpl } = setup();

        engine.bootstrap();
        await documentImpl.listeners.touchstart[0]();

        const [playback] = FakeAudioContext.instances;

        const call = { id: 1, audio: { data: [1, 2, 3, 4] } };
        const ended = [];

        const handle = engine.playCall(call, { onEnded: () => ended.push(true) });

        const [arrayBuffer] = playback.decoded;
        assert.deepEqual([...new Uint8Array(arrayBuffer)], [1, 2, 3, 4]);

        const [source] = playback.sources;
        assert.equal(source.started, true);
        assert.deepEqual(source.connections, [playback.destination]);

        source.onended();
        assert.deepEqual(ended, [true]);

        handle.stop();
        assert.equal(source.stopped, true);
    });

    it('the stop handle silences the recording and its ended callback never fires afterward', async () => {
        const { engine, documentImpl } = setup();

        engine.bootstrap();
        await documentImpl.listeners.touchstart[0]();

        const [playback] = FakeAudioContext.instances;

        const call = { id: 1, audio: { data: [1] } };
        const ended = [];

        const handle = engine.playCall(call, { onEnded: () => ended.push(true) });

        const [source] = playback.sources;
        assert.equal(source.started, true);

        handle.stop();

        assert.equal(source.stopped, true, 'the source is stopped');
        assert.equal(source.disconnected, true, 'and disconnected from the destination');
        assert.deepEqual(ended, [], 'the handle disarms the ended callback before stopping, so the browser-fired ended never reaches the listener');
    });

    it('is a no-op before bootstrap (no audio context yet)', () => {
        const { engine } = setup();

        const handle = engine.playCall({ id: 1, audio: { data: [0] } }, {});

        assert.equal(handle, undefined);
        assert.equal(FakeAudioContext.instances.length, 0);
    });

    it('plays oscillator sequences through the interactive context', async () => {
        const { engine, documentImpl } = setup();

        engine.bootstrap();
        await documentImpl.listeners.keydown[0]();

        const [, interactive] = FakeAudioContext.instances;

        const promise = engine.playOscillators([{ begin: 0, end: 0.1, frequency: 800, type: 'square' }]);
        interactive.oscillators[0].onended();
        await promise;

        assert.equal(interactive.oscillators.length, 1);
        assert.equal(FakeAudioContext.instances[0].oscillators.length, 0);
    });
});

describe('infrastructure/audio-engine — a stop that outranks a decode in flight', () => {
    it('a recording stopped while its decode is in flight never enters the graph and leaves no clock', async () => {
        const { engine, documentImpl } = setupDeferredDecode();

        engine.bootstrap();
        await documentImpl.listeners.touchstart[0]();

        const [playback] = FakeAudioContext.instances;

        const handle = engine.playCall({ id: 1, audio: { data: [1, 2, 3] } }, {});

        assert.equal(playback.pendingDecode.length, 1, 'a real decode answers in a later task');
        assert.equal(playback.sources.length, 0, 'nothing has entered the graph yet');

        // the listener has already moved on: the use case calls stop and drops the
        // handle, so no later call can reach whatever this recording starts.
        handle.stop();
        playback.completeDecode();

        assert.deepEqual(
            playback.sources.filter((source) => source.started && !source.stopped),
            [],
            'the stopped recording leaves no source playing that nothing can silence',
        );
        assert.equal(engine.playbackElapsed(), null, 'a stopped recording has no clock');
    });
});

describe('infrastructure/audio-engine — download', () => {
    it('triggers a base64 data-URI anchor download with the call filename', () => {
        const { engine, documentImpl } = setup();

        engine.download({ audio: { data: [72, 105] }, audioName: 'call.wav', audioType: 'audio/wav' });

        const anchor = documentImpl.created[0];
        assert.equal(anchor.attributes.href, `data:audio/wav;base64,${btoa('Hi')}`);
        assert.equal(anchor.attributes.download, 'call.wav');
        assert.equal(anchor.clicked, true);
        assert.equal(documentImpl.appended.length, 0);
    });

    it('falls back to unknown.dat and audio/*', () => {
        const { engine, documentImpl } = setup();

        engine.download({ audio: { data: [65] } });

        const anchor = documentImpl.created[0];
        assert.equal(anchor.attributes.download, 'unknown.dat');
        assert.equal(anchor.attributes.href, `data:audio/*;base64,${btoa('A')}`);
    });
});

describe('infrastructure/audio-engine — timing constants', () => {
    it('reproduces the legacy 1 s playback gap and 500 ms progress tick', () => {
        assert.equal(PLAYBACK_GAP_MS, 1000);
        assert.equal(PROGRESS_TICK_MS, 500);
    });
});
