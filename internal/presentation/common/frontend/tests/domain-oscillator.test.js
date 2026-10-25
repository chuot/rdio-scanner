// Sequencing of keypad and alert oscillator schedules.
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { scheduleOscillators } from '../domain/oscillator.js';

function createMockAudioContext() {
    const oscillators = [];

    const gain = { gain: { value: null }, connections: [], connect: (target) => gain.connections.push(target) };

    return {
        currentTime: 10,
        destination: { id: 'destination' },
        gains: [gain],
        oscillators,
        createGain: () => gain,
        createOscillator: () => {
            const osc = {
                connections: [],
                frequency: { value: null },
                startedAt: null,
                stoppedAt: null,
                type: null,
                onended: null,
                connect: (target) => osc.connections.push(target),
                start: (t) => (osc.startedAt = t),
                stop: (t) => (osc.stoppedAt = t),
            };
            oscillators.push(osc);
            return osc;
        },
    };
}

describe('domain/oscillator — sequence scheduling', () => {
    it('schedules every segment with gain 0.1, absolute start/stop times, frequency and type', async () => {
        const ctx = createMockAudioContext();
        const seq = [
            { begin: 0, end: 0.2, frequency: 1000, type: 'square' },
            { begin: 0.3, end: 0.5, frequency: 1500, type: 'sine' },
        ];

        const promise = scheduleOscillators(ctx, seq);
        ctx.oscillators[1].onended();
        await promise;

        assert.equal(ctx.gains[0].gain.value, 0.1);
        assert.deepEqual(ctx.gains[0].connections, [ctx.destination]);

        const [a, b] = ctx.oscillators;
        assert.equal(a.frequency.value, 1000);
        assert.equal(a.type, 'square');
        assert.equal(a.startedAt, 10);
        assert.equal(a.stoppedAt, 10.2);
        assert.equal(b.startedAt, 10.3);
        assert.equal(b.stoppedAt, 10.5);
        assert.deepEqual(a.connections, [ctx.gains[0]]);
    });

    it('resolves when the last oscillator ends', async () => {
        const ctx = createMockAudioContext();
        let resolved = false;

        const promise = scheduleOscillators(ctx, [{ begin: 0, end: 1, frequency: 500, type: 'square' }])
            .then(() => (resolved = true));

        await new Promise((r) => setImmediate(r));
        assert.equal(resolved, false);

        ctx.oscillators[0].onended();
        await promise;
        assert.equal(resolved, true);
    });

    it('resolves immediately with no context or empty sequence', async () => {
        await scheduleOscillators(null, [{ begin: 0, end: 1, frequency: 1, type: 'sine' }]);
        await scheduleOscillators(createMockAudioContext(), []);
        await scheduleOscillators(createMockAudioContext(), undefined);
    });
});
