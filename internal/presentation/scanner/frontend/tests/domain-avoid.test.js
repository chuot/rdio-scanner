import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { expireAvoid, nextAvoidStep } from '../domain/avoid.js';
import { decodeMap, avoidMinutes, isAvoided, toggleTalkgroup } from '../domain/livefeed-map.js';

describe('domain/avoid — cycle state machine', () => {
    it('first press on an active talkgroup avoids it indefinitely', () => {
        assert.deepEqual(nextAvoidStep({ avoided: false, minutes: 0 }), { status: false });
    });

    it('second press starts the 30 minute timed avoid', () => {
        assert.deepEqual(nextAvoidStep({ avoided: true, minutes: 0 }), { minutes: 30, status: false });
    });

    it('the cycle walks 30M → 60M → 120M', () => {
        assert.deepEqual(nextAvoidStep({ avoided: true, minutes: 30 }), { minutes: 60, status: false });
        assert.deepEqual(nextAvoidStep({ avoided: true, minutes: 60 }), { minutes: 120, status: false });
    });

    it('pressing after 120M re-activates the talkgroup', () => {
        assert.deepEqual(nextAvoidStep({ avoided: true, minutes: 120 }), { status: true });
    });
});

describe('domain/avoid — timed expiry', () => {
    it('applying a timed avoid stores minutes and deactivates', () => {
        const map = toggleTalkgroup(decodeMap({ 10: { 100: true } }), 10, 100, { minutes: 30, status: false });

        assert.equal(map[10][100].active, false);
        assert.equal(avoidMinutes(map, 10, 100), 30);
    });

    it('expireAvoid re-activates the talkgroup and clears the timer', () => {
        const map = { 10: { 100: { active: false, minutes: 30 } } };
        const next = expireAvoid(map, 10, 100);

        assert.equal(next[10][100].active, true);
        assert.equal(next[10][100].minutes, undefined);
        assert.equal(isAvoided(next, 10, 100), false);
    });
});
