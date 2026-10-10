import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { migrateLegacyConfig } from '../domain/config-legacy-keys.js';

describe('domain/config-legacy-keys — old config file migration', () => {
    const legacy = {
        access: [{ _id: 1, systems: [2] }],
        apiKeys: [{ _id: 3, key: 'k' }],
        dirWatch: [{ _id: 4, dir: '/d' }],
        downstreams: [{ _id: 5, apiKey: 'remote', url: 'ws://x' }],
        groups: [{ _id: 6, label: 'Zeta' }, { _id: 7, label: 'Alpha' }],
        tags: [{ _id: 8, label: 'T2' }, { _id: 9, label: 'T1' }],
        systems: [
            {
                _id: 10,
                id: 77,
                label: 'SYS',
                talkgroups: [
                    { id: 88, groupId: 6, label: 'TG1' },
                    { id: 89, label: 'TG2' },
                ],
            },
        ],
    };

    it('maps _id to id on every collection', () => {
        const migrated = migrateLegacyConfig(legacy);

        assert.equal(migrated.access[0].id, 1);
        assert.equal(migrated.dirwatch[0].id, 4);
        assert.equal(migrated.downstreams[0].id, 5);
        assert.equal(migrated.groups[0].id, 7);
    });

    it('renames apiKeys to apikeys and maps their _id (fixes the legacy loop bug)', () => {
        const migrated = migrateLegacyConfig(legacy);

        assert.equal(migrated.apikeys.length, 1);
        assert.equal(migrated.apikeys[0].id, 3);
        assert.equal(migrated.apikeys[0].key, 'k');
    });

    it('renames dirWatch to dirwatch and apiKey to apikey on downstreams', () => {
        const migrated = migrateLegacyConfig(legacy);

        assert.equal(migrated.dirwatch[0].dir, '/d');
        assert.equal(migrated.downstreams[0].apikey, 'remote');
    });

    it('sorts groups and tags by label', () => {
        const migrated = migrateLegacyConfig(legacy);

        assert.deepEqual(migrated.groups.map((g) => g.label), ['Alpha', 'Zeta']);
        assert.deepEqual(migrated.tags.map((t) => t.label), ['T1', 'T2']);
    });

    it('maps system id to systemRef, groupId to groupIds, talkgroup id to talkgroupRef', () => {
        const migrated = migrateLegacyConfig(legacy);
        const system = migrated.systems[0];
        const tg1 = system.talkgroups.find((tg) => tg.label === 'TG1');
        const tg2 = system.talkgroups.find((tg) => tg.label === 'TG2');

        assert.equal(system.systemRef, 77);
        assert.equal(system.id, 10);
        assert.equal(tg1.talkgroupRef, 88);
        assert.deepEqual(tg1.groupIds, [6]);
        assert.equal(tg1.id, undefined);
        assert.equal(tg2.talkgroupRef, 89);
        assert.equal(tg2.groupIds, undefined);
    });
});
