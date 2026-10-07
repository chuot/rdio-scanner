import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { PLAYBACK_GAP_MS, PROGRESS_TICK_MS, createAudioEngine } from '../scanner/infrastructure/audio-engine.js';

class FakeAudioContext {
    static instances = [];

    constructor(options) {
        this.options = options;
        this.state = 'suspended';
        this.resumeCalls = 0;
        this.onstatechange = null;
        this.decoded = [];
        this.sources = [];
        this.oscillators = [];
        this.destination = { id: 'destination' };
        FakeAudioContext.instances.push(this);
    }

    resume() {
        this.resumeCalls++;
        this.state = 'running';
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
            stop: () => (source.stopped = true),
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
