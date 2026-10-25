// tests for domain/search-filters: cascading recomputation
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { computeFilterOptions, resolveCallSearchCriteria } from '../domain/search-filters.js';

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

// the criteria the call history is searched by. the operator reads
// labels; the history is stored under references, so a system or a
// talkgroup must say which one, and a group or tag filters by label.

describe('domain/search-filters — the chosen filters become the criteria the history is searched by', () => {
    it('names the reference of the system the chosen system label stands for', () => {
        assert.deepEqual(resolveCallSearchCriteria(config, { system: 'SYS2' }), { system: 20 });
    });

    it('names the references of the chosen talkgroup and the system it sits under', () => {
        assert.deepEqual(resolveCallSearchCriteria(config, { system: 'SYS1', talkgroup: 'TG2' }), { system: 10, talkgroup: 101 });
    });

    it('keeps a group and a tag as the labels the history is filtered by', () => {
        assert.deepEqual(resolveCallSearchCriteria(config, { group: 'Police', tag: 'Dispatch' }), { group: 'Police', tag: 'Dispatch' });
    });

    it('asks for the whole history when the operator chose nothing', () => {
        assert.deepEqual(resolveCallSearchCriteria(config, {}), {});
    });

    it('leaves the history unfiltered by system when the chosen label names no system', () => {
        assert.deepEqual(resolveCallSearchCriteria(config, { system: 'Withdrawn system' }), {});
    });

    it('leaves the history unfiltered when a talkgroup is named with no system standing above it', () => {
        assert.deepEqual(resolveCallSearchCriteria(config, { talkgroup: 'TG1' }), {});
    });

    it('filters the system alone when the chosen talkgroup is not under it', () => {
        assert.deepEqual(resolveCallSearchCriteria(config, { system: 'SYS1', talkgroup: 'TG3' }), { system: 10 });
    });

    it('sends no criterion for a reference of zero', () => {
        const unnumbered = { systems: [{ id: 0, label: 'No reference', talkgroups: [{ id: 0, label: 'No reference talkgroup', groups: [], tag: 'Dispatch' }] }], groups: {}, tags: {} };

        assert.deepEqual(resolveCallSearchCriteria(unnumbered, { system: 'No reference', talkgroup: 'No reference talkgroup' }), {});
    });

    it('takes the first system when two systems carry the chosen label', () => {
        const twins = {
            systems: [
                { id: 30, label: 'Dispatch', talkgroups: [{ id: 300, label: 'Ground', groups: [], tag: 'Dispatch' }] },
                { id: 31, label: 'Dispatch', talkgroups: [{ id: 310, label: 'Ground', groups: [], tag: 'Dispatch' }] },
            ],
            groups: {},
            tags: {},
        };

        assert.deepEqual(resolveCallSearchCriteria(twins, { system: 'Dispatch', talkgroup: 'Ground' }), { system: 30, talkgroup: 300 });
    });

    it('carries the date and the sort the operator set alongside the chosen filters', () => {
        const criteria = resolveCallSearchCriteria(config, { system: 'SYS1', group: 'Fire' });

        assert.equal(criteria.system, 10);
        assert.equal(criteria.group, 'Fire');
        assert.equal(criteria.talkgroup, undefined);
        assert.equal(criteria.tag, undefined);
    });
});
