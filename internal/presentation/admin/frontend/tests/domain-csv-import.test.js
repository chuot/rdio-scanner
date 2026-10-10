import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { importTalkgroups, importUnits, parseCsv } from '../domain/csv-import.js';

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
