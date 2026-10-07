import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { holdSystemMap, holdTalkgroupMap } from '../domain/hold.js';
import { decodeMap } from '../domain/livefeed-map.js';

describe('domain/hold — HOLD SYS snapshot semantics', () => {
    const map = decodeMap({ 10: { 100: true, 101: false }, 20: { 200: true } });
    const call = { system: 10, talkgroup: 100 };

    it('keeps the current system selection and deactivates every other system', () => {
        const held = holdSystemMap(map, call);

        assert.equal(held[10][100].active, true);
        assert.equal(held[10][101].active, false);
        assert.equal(held[20][200].active, false);
    });

    it('a fully inactive current system is switched on as a whole', () => {
        const allOff = decodeMap({ 10: { 100: false, 101: false }, 20: { 200: true } });
        const held = holdSystemMap(allOff, call);

        assert.equal(held[10][100].active, true);
        assert.equal(held[10][101].active, true);
        assert.equal(held[20][200].active, false);
    });

    it('does not mutate the input map (snapshot source stays intact)', () => {
        holdSystemMap(map, call);

        assert.equal(map[20][200].active, true);
    });
});

describe('domain/hold — HOLD TG snapshot semantics', () => {
    const map = decodeMap({ 10: { 100: true, 101: true }, 20: { 200: true } });
    const call = { system: 10, talkgroup: 101 };

    it('keeps only the current talkgroup active', () => {
        const held = holdTalkgroupMap(map, call);

        assert.equal(held[10][100].active, false);
        assert.equal(held[10][101].active, true);
        assert.equal(held[20][200].active, false);
    });
});
