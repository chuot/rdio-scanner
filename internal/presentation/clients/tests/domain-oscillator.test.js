import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { scheduleOscillators } from '../shared/domain/oscillator.js';
import { resolveAlert, resolveLedColor } from '../src/domain/alert-priority.js';

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

describe('domain/alert-priority — group → tag → system → talkgroup', () => {
    const config = {
        alerts: { A: [], B: [], C: [], D: [] },
        groupsData: [{ id: 1, label: 'G1', alert: 'A' }, { id: 2, label: 'G2' }],
        tagsData: [{ id: 3, label: 'T1', alert: 'B' }, { id: 4, label: 'T2' }],
    };

    it('group alert wins over everything else', () => {
        const call = {
            groupsData: [{ label: 'G2' }, { label: 'G1' }],
            tagData: { label: 'T1' },
            systemData: { alert: 'C' },
            talkgroupData: { alert: 'D' },
        };

        assert.equal(resolveAlert(call, config), 'A');
    });

    it('tag alert wins when no group has one', () => {
        const call = {
            groupsData: [{ label: 'G2' }],
            tagData: { label: 'T1' },
            systemData: { alert: 'C' },
            talkgroupData: { alert: 'D' },
        };

        assert.equal(resolveAlert(call, config), 'B');
    });

    it('system alert wins over talkgroup alert', () => {
        const call = {
            groupsData: [{ label: 'G2' }],
            tagData: { label: 'T2' },
            systemData: { alert: 'C' },
            talkgroupData: { alert: 'D' },
        };

        assert.equal(resolveAlert(call, config), 'C');
    });

    it('talkgroup alert is last in priority', () => {
        const call = {
            groupsData: [{ label: 'G2' }],
            tagData: { label: 'T2' },
            systemData: {},
            talkgroupData: { alert: 'D' },
        };

        assert.equal(resolveAlert(call, config), 'D');
    });

    it('no alert when nothing defines one', () => {
        assert.equal(resolveAlert({ groupsData: [], tagData: undefined }, config), undefined);
    });
});

describe('domain/alert-priority — LED color resolution', () => {
    const colors = ['blue', 'cyan', 'green', 'magenta', 'orange', 'red', 'white', 'yellow'];

    it('group led wins, then tag, system, talkgroup', () => {
        assert.equal(
            resolveLedColor({
                groupsData: [{ led: 'red' }],
                tagData: { led: 'blue' },
                systemData: { led: 'cyan' },
                talkgroupData: { led: 'orange' },
            }, colors),
            'red',
        );

        assert.equal(
            resolveLedColor({ groupsData: [], tagData: { led: 'blue' }, systemData: { led: 'cyan' } }, colors),
            'blue',
        );

        assert.equal(
            resolveLedColor({ groupsData: [], systemData: { led: 'cyan' }, talkgroupData: { led: 'orange' } }, colors),
            'cyan',
        );

        assert.equal(
            resolveLedColor({ groupsData: [], talkgroupData: { led: 'orange' } }, colors),
            'orange',
        );
    });

    it('falls back to green for missing or invalid colors', () => {
        assert.equal(resolveLedColor({ groupsData: [] }, colors), 'green');
        assert.equal(resolveLedColor({ talkgroupData: { led: 'pink' } }, colors), 'green');
    });
});
