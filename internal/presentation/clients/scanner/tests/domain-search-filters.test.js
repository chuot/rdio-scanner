import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { computeFilterOptions } from '../domain/search-filters.js';

const config = {
    groups: {
        Fire: { 10: [100] },
        Police: { 10: [101], 20: [200] },
    },
    tags: {
        Dispatch: { 10: [100, 101], 20: [200] },
        Tactical: { 20: [201] },
    },
    systems: [
        {
            id: 10,
            label: 'SYS1',
            talkgroups: [
                { id: 100, label: 'TG1', groups: ['Fire'], tag: 'Dispatch' },
                { id: 101, label: 'TG2', groups: ['Police'], tag: 'Dispatch' },
            ],
        },
        {
            id: 20,
            label: 'SYS2',
            talkgroups: [
                { id: 200, label: 'TG3', groups: ['Police'], tag: 'Dispatch' },
                { id: 201, label: 'TG4', groups: [], tag: 'Tactical' },
            ],
        },
    ],
};

describe('domain/search-filters — cascading recomputation', () => {
    it('with no selection, every option is available', () => {
        const options = computeFilterOptions(config, {});

        assert.deepEqual(options.systems, ['SYS1', 'SYS2']);
        assert.deepEqual(options.talkgroups, []);
        assert.deepEqual(options.groups, ['Fire', 'Police']);
        assert.deepEqual(options.tags, ['Dispatch', 'Tactical']);
    });

    it('selecting a system restricts talkgroups to that system and drops unrelated systems', () => {
        const options = computeFilterOptions(config, { system: 'SYS2' });

        assert.deepEqual(options.systems, ['SYS1', 'SYS2']);
        assert.deepEqual(options.talkgroups, ['TG3', 'TG4']);
        assert.deepEqual(options.groups, ['Police']);
        assert.deepEqual(options.tags, ['Dispatch', 'Tactical']);
    });

    it('selecting a group restricts systems and talkgroups', () => {
        const options = computeFilterOptions(config, { group: 'Fire' });

        assert.deepEqual(options.systems, ['SYS1']);
        assert.deepEqual(options.groups, ['Fire', 'Police']);
    });

    it('selecting a system and a group intersects talkgroups but keeps group-compatible systems listed', () => {
        const options = computeFilterOptions(config, { system: 'SYS2', group: 'Police' });

        assert.deepEqual(options.systems, ['SYS1', 'SYS2']);
        assert.deepEqual(options.talkgroups, ['TG3']);
        assert.deepEqual(options.tags, ['Dispatch']);
    });

    it('selecting a tag restricts groups to those sharing a talkgroup with it', () => {
        const options = computeFilterOptions(config, { tag: 'Tactical' });

        assert.deepEqual(options.systems, ['SYS2']);
        assert.deepEqual(options.groups, []);
        assert.deepEqual(options.tags, ['Dispatch', 'Tactical']);
    });

    it('selecting a talkgroup restricts group and tag lists to its own', () => {
        const options = computeFilterOptions(config, { system: 'SYS1', talkgroup: 'TG1' });

        assert.deepEqual(options.groups, ['Fire']);
        assert.deepEqual(options.tags, ['Dispatch']);
    });
});
