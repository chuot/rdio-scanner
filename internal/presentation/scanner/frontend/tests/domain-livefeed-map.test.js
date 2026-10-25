// tests for domain/livefeed-map: encoding the livefeed map
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
    avoidMinutes,
    decodeMap,
    encodeMap,
    isAvoided,
    isPatched,
    legacyStorageKey,
    rebuildMap,
    storageKey,
    toggleAll,
    toggleSystem,
    toggleTalkgroup,
} from '../domain/livefeed-map.js';

describe('domain/livefeed-map — encoding', () => {
    it('encodes the internal map to the wire/storage shape (active booleans only)', () => {
        const map = { 10: { 100: { active: true, minutes: 30 }, 101: { active: false } } };

        assert.deepEqual(encodeMap(map), { 10: { 100: true, 101: false } });
    });

    it('decodes the wire/storage shape into the internal map', () => {
        const map = decodeMap({ 10: { 100: true, 101: false } });

        assert.deepEqual(map, { 10: { 100: { active: true }, 101: { active: false } } });
    });

    it('namespaces the storage key by instanceId and keeps the legacy fallback key', () => {
        assert.equal(storageKey('default'), 'rdio-scanner-lfm-default');
        assert.equal(storageKey('profile1'), 'rdio-scanner-lfm-profile1');
        assert.equal(legacyStorageKey(), 'rdio-scanner');
    });
});

const base = decodeMap({ 10: { 100: true, 101: false }, 20: { 200: true } });

describe('domain/livefeed-map — operations', () => {
    it('toggleTalkgroup flips active state and clears timed-avoid minutes', () => {
        const map = toggleTalkgroup(base, 10, 100);

        assert.equal(map[10][100].active, false);
        assert.equal(map[10][100].minutes, undefined);

        const withMinutes = { 10: { 100: { active: false, minutes: 30 } } };
        const restored = toggleTalkgroup(withMinutes, 10, 100, { status: true });

        assert.equal(restored[10][100].active, true);
        assert.equal(restored[10][100].minutes, undefined);
    });

    it('toggleSystem flips every talkgroup of a system', () => {
        const map = toggleSystem(base, 10);

        assert.equal(map[10][100].active, false);
        assert.equal(map[10][101].active, true);
        assert.equal(map[20][200].active, true);
    });

    it('toggleAll sets an explicit status on every talkgroup', () => {
        const off = toggleAll(base, false);
        const on = toggleAll(base, true);

        assert.equal(off[10][100].active, false);
        assert.equal(off[10][101].active, false);
        assert.equal(off[20][200].active, false);
        assert.equal(on[10][101].active, true);
    });

    it('does not mutate the input map', () => {
        toggleTalkgroup(base, 10, 100);
        assert.equal(base[10][100].active, true);
    });
});

describe('domain/livefeed-map — queries', () => {
    it('isAvoided is true when the entry is missing or not active', () => {
        assert.equal(isAvoided(base, 10, 101), true);
        assert.equal(isAvoided(base, 10, 100), false);
        assert.equal(isAvoided(base, 99, 99), true);
    });

    it('avoidMinutes reports the timed-avoid minutes', () => {
        const map = { 10: { 100: { active: false, minutes: 30 } } };

        assert.equal(avoidMinutes(map, 10, 100), 30);
        assert.equal(avoidMinutes(base, 10, 101), 0);
    });

    it('isPatched is true when the call is avoided but a patched talkgroup is active', () => {
        const call = { system: 10, talkgroup: 101, patches: [100] };

        assert.equal(isPatched(base, call), true);
        assert.equal(isPatched(base, { system: 10, talkgroup: 100, patches: [101] }), false);
    });
});

describe('domain/livefeed-map — rebuild from config', () => {
    const systems = [
        { id: 10, talkgroups: [{ id: 100, groups: ['G1'], tag: 'T1' }, { id: 101, groups: ['G2'], tag: 'T1' }] },
    ];

    it('keeps existing entries and defaults new ones to active', () => {
        const stored = decodeMap({ 10: { 100: false } });
        const map = rebuildMap(systems, stored, []);

        assert.equal(map[10][100].active, false);
        assert.equal(map[10][101].active, true);
    });

    it('defaults a new talkgroup to inactive when its group category is off', () => {
        const categories = [{ label: 'G1', status: 'off', type: 'group' }];
        const map = rebuildMap(systems, {}, categories);

        assert.equal(map[10][100].active, false);
        assert.equal(map[10][101].active, true);
    });

    it('defaults a new talkgroup to inactive when its tag category is off', () => {
        const categories = [{ label: 'T1', status: 'off', type: 'tag' }];
        const map = rebuildMap(systems, {}, categories);

        assert.equal(map[10][100].active, false);
        assert.equal(map[10][101].active, false);
    });

    it('drops entries that no longer exist in the config', () => {
        const stored = decodeMap({ 10: { 100: true, 999: true } });
        const map = rebuildMap(systems, stored, []);

        assert.equal(map[10][999], undefined);
    });
});
