import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { deriveCategories, toggleCategory } from '../src/domain/categories.js';
import { decodeMap } from '../shared/domain/livefeed-map.js';

// groups map: { label: { systemRef: [talkgroupRef...] } }
const groupsMap = {
    Fire: { 10: [100, 101] },
    Police: { 10: [102], 20: [200] },
};

const systems = [
    { id: 10, talkgroups: [{ id: 100, groups: ['Fire'], tag: 'T1' }, { id: 101, groups: ['Fire'], tag: 'T1' }, { id: 102, groups: ['Police'], tag: 'T1' }] },
    { id: 20, talkgroups: [{ id: 200, groups: ['Police'], tag: 'T1' }] },
];

describe('domain/categories — tri-state derivation', () => {
    it('derives off when no talkgroup of the category is active', () => {
        const map = decodeMap({ 10: { 100: false, 101: false, 102: true }, 20: { 200: true } });
        const categories = deriveCategories(groupsMap, map);

        assert.deepEqual(categories.map((c) => [c.label, c.status, c.type]), [
            ['Fire', 'off', 'group'],
            ['Police', 'on', 'group'],
        ]);
    });

    it('derives partial when some talkgroups are active', () => {
        const map = decodeMap({ 10: { 100: true, 101: false, 102: true }, 20: { 200: true } });
        const categories = deriveCategories(groupsMap, map);

        assert.equal(categories.find((c) => c.label === 'Fire').status, 'partial');
    });

    it('sorts categories by label', () => {
        const map = decodeMap({ 10: { 100: true, 101: true, 102: true }, 20: { 200: true } });
        const categories = deriveCategories({ Zeta: { 10: [100] }, Alpha: { 10: [101] } }, map);

        assert.deepEqual(categories.map((c) => c.label), ['Alpha', 'Zeta']);
    });
});

describe('domain/categories — toggle semantics', () => {
    const map = decodeMap({ 10: { 100: true, 101: false, 102: true }, 20: { 200: true } });

    it('pressing an on category makes all its talkgroups inactive', () => {
        const category = { label: 'Police', status: 'on', type: 'group' };
        const next = toggleCategory(map, systems, category);

        assert.equal(next[10][102].active, false);
        assert.equal(next[20][200].active, false);
        assert.equal(next[10][100].active, true);
    });

    it('pressing an off or partial category makes all its talkgroups active', () => {
        const off = toggleCategory(map, systems, { label: 'Fire', status: 'off', type: 'group' });
        assert.equal(off[10][100].active, true);
        assert.equal(off[10][101].active, true);

        const partial = toggleCategory(map, systems, { label: 'Fire', status: 'partial', type: 'group' });
        assert.equal(partial[10][100].active, true);
        assert.equal(partial[10][101].active, true);
    });

    it('toggling clears timed-avoid minutes on affected talkgroups', () => {
        const timed = { 10: { 100: { active: false, minutes: 30 }, 101: { active: true } } };
        const next = toggleCategory(timed, systems, { label: 'Fire', status: 'off', type: 'group' });

        assert.equal(next[10][100].minutes, 0);
        assert.equal(next[10][100].active, true);
    });

    it('tag categories toggle the talkgroups carrying the tag label', () => {
        const next = toggleCategory(map, systems, { label: 'T1', status: 'on', type: 'tag' });

        assert.equal(next[10][100].active, false);
        assert.equal(next[10][102].active, false);
        assert.equal(next[20][200].active, false);
    });
});
