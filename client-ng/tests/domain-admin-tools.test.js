import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { computeTodos } from '../src/domain/todos.js';
import { importTalkgroups, importUnits, parseCsv } from '../src/domain/csv-import.js';
import { migrateLegacyConfig } from '../src/domain/config-legacy-keys.js';

describe('domain/todos — exact legacy messages', () => {
    const fullConfig = { systems: [{ id: 1 }], apikeys: [{ id: 1 }], dirwatch: [] };

    it('warns about the default admin password first', () => {
        const todos = computeTodos(fullConfig, { passwordNeedChange: true });

        assert.deepEqual(todos, [
            {
                level: 'warn',
                message: 'You are using the default admin password, please change it from the tools / admin password menu.',
            },
        ]);
    });

    it('reports missing systems', () => {
        const todos = computeTodos({ systems: [], apikeys: [{ id: 1 }], dirwatch: [] }, {});

        assert.deepEqual(todos.map((t) => t.level), ['info']);
        assert.equal(todos[0].message, 'No systems defined. You can define one from the systems menu, or let the auto populate feature create it for you if the global option is turned on.');
    });

    it('reports missing ingest sources when both apikeys and dirwatch are empty', () => {
        const todos = computeTodos({ systems: [{ id: 1 }], apikeys: [], dirwatch: [] }, {});

        assert.equal(todos.length, 1);
        assert.equal(todos[0].message, 'No apikeys or dirwatch defined. Set at least one to allow ingesting audio files.');
    });

    it('keeps the legacy order: password, systems, ingest', () => {
        const todos = computeTodos({}, { passwordNeedChange: true });

        assert.deepEqual(todos.map((t) => t.level), ['warn', 'info', 'info']);
    });

    it('no todos for a fully configured server', () => {
        assert.deepEqual(computeTodos(fullConfig, { passwordNeedChange: false }), []);
    });
});

describe('domain/csv-import — talkgroup import', () => {
    const csv = [
        '"100","freq","Fire Dispatch 1","freq2","Fire Dispatch one","Dispatch","Fire"',
        '"101","freq","Fire Dispatch 2","freq2","Fire Dispatch two","Dispatch","Fire"',
        '"101","dup","dupe row","x","y","z","w"',
        'not-a-number,x,x,x,x,x,x',
        '',
    ].join('\n');

    it('parses rows, strips quotes, filters non-numeric ids and dedupes by id', () => {
        const rows = parseCsv(csv);

        assert.deepEqual(rows.map((r) => r[0]), ['100', '101']);
    });

    it('creates missing groups and tags with next ids and prepends talkgroups', () => {
        const config = {
            groups: [{ id: 4, label: 'Existing' }],
            tags: [],
            systems: [{ id: 7, label: 'SYS', talkgroups: [{ id: 50, label: 'Old' }] }],
        };
        const system = config.systems[0];

        const rows = parseCsv(csv);
        const next = importTalkgroups(config, system, rows);

        assert.deepEqual(next.groups.map((g) => g.label), ['Existing', 'Fire']);
        assert.equal(next.groups[1].id, 5);

        assert.deepEqual(next.tags, [{ id: 1, label: 'Dispatch' }]);

        // legacy unshifts each row: last parsed row ends up first
        assert.deepEqual(next.systems[0].talkgroups.map((tg) => tg.label), ['Fire Dispatch 2', 'Fire Dispatch 1', 'Old']);
        assert.deepEqual(next.systems[0].talkgroups[0], {
            talkgroupRef: 101,
            label: 'Fire Dispatch 2',
            name: 'Fire Dispatch two',
            groupIds: [5],
            tagId: 1,
        });
    });

    it('reuses existing groups and tags instead of creating duplicates', () => {
        const config = {
            groups: [{ id: 2, label: 'Fire' }],
            tags: [{ id: 3, label: 'Dispatch' }],
            systems: [{ id: 7, talkgroups: [] }],
        };

        const next = importTalkgroups(config, config.systems[0], parseCsv(csv));

        assert.equal(next.groups.length, 1);
        assert.deepEqual(next.systems[0].talkgroups[0].groupIds, [2]);
        assert.equal(next.systems[0].talkgroups[0].tagId, 3);
    });
});

describe('domain/csv-import — unit import', () => {
    it('parses unit rows and appends units with unitRef and order', () => {
        const config = { systems: [{ id: 7, units: [{ id: 1, label: 'Old Unit' }] }] };

        const rows = parseCsv('"5001","Unit One"\n5002,Unit Two\n');
        const next = importUnits(config, config.systems[0], rows);

        assert.deepEqual(next.systems[0].units.map((u) => u.label), ['Old Unit', 'Unit One', 'Unit Two']);
        assert.deepEqual(next.systems[0].units[1], { unitRef: 5001, label: 'Unit One', order: 1 });
        assert.equal(next.systems[0].units[2].order, 2);
    });
});

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
