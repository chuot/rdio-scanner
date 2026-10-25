// Listener config normalization and the admin config round-trip.
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
    createAdminWorkingCopy,
    mergeAdminConfig,
    normalizeListenerConfig,
    serializeAdminConfig,
    transformCall,
} from '../domain/config.js';

describe('domain/config — listener config normalization', () => {
    it('applies defensive defaults to a partial CFG payload', () => {
        const config = normalizeListenerConfig({});

        assert.equal(config.branding, '');
        assert.equal(config.email, '');
        assert.deepEqual(config.groups, {});
        assert.deepEqual(config.groupsData, []);
        assert.deepEqual(config.keypadBeeps, {});
        assert.equal(config.showListenersCount, false);
        assert.deepEqual(config.systems, []);
        assert.deepEqual(config.tags, {});
        assert.deepEqual(config.tagsData, []);
    });

    it('keeps well-typed values and drops the retired keys from the CFG payload', () => {
        const raw = {
            branding: 'My Scanner',
            dimmerDelay: 3000,
            email: 'a@b.c',
            playbackGoesLive: true,
            showListenersCount: true,
            time12hFormat: true,
            systems: [{ id: 1, label: 'S1', talkgroups: [] }],
        };

        const config = normalizeListenerConfig(raw);

        assert.equal(config.branding, 'My Scanner');
        assert.equal(config.email, 'a@b.c');
        assert.equal(config.showListenersCount, true);
        assert.equal(config.systems.length, 1);

        // The retired options never reach the listener state.
        assert.equal(config.dimmerDelay, undefined);
        assert.equal(config.playbackGoesLive, undefined);
        assert.equal(config.time12hFormat, undefined);
    });
});

describe('domain/config — transformCall', () => {
    const config = normalizeListenerConfig({
        groupsData: [{ id: 1, label: 'G1' }],
        tagsData: [{ id: 2, label: 'T1' }],
        systems: [
            {
                id: 10,
                label: 'SYS',
                talkgroups: [
                    { id: 100, label: 'TG1', name: 'Name 1', groups: ['G1'], tag: 'T1', frequency: 770506250 },
                    { id: 101, label: 'TG2', name: 'Name 2', groups: [], tag: 'T1' },
                ],
            },
        ],
    });

    it('resolves systemData, talkgroupData, groupsData, tagData and frequency', () => {
        const call = transformCall(config, { id: 1, system: 10, talkgroup: 100 });

        assert.equal(call.systemData.label, 'SYS');
        assert.equal(call.talkgroupData.label, 'TG1');
        assert.equal(call.frequency, 770506250);
        assert.deepEqual(call.groupsData.map((g) => g.label), ['G1']);
        assert.equal(call.tagData.label, 'T1');
    });

    it('leaves unknown references unresolved', () => {
        const call = transformCall(config, { id: 1, system: 99, talkgroup: 98 });

        assert.equal(call.systemData, undefined);
        assert.equal(call.talkgroupData, undefined);
    });
});

describe('domain/config — admin round-trip', () => {
    const baseline = {
        access: [],
        alerts: undefined,
        apikeys: [],
        dirwatch: [],
        downstreams: [],
        groups: [{ id: 1, label: 'G1' }],
        options: {
            adminPassword: 'secret-hash',
            branding: 'X',
            dimmerDelay: 5000,
            secret: 'server-secret',
        },
        systems: [
            {
                id: 7,
                label: 'SYS',
                systemRef: 10,
                talkgroups: [
                    { id: 100, talkgroupRef: 100, label: 'TG1', name: 'N', talkgroup: 3, serverOnlyField: 42 },
                ],
            },
        ],
        tags: [],
        version: '7.0.0-dev',
        unknownTopLevel: [{ keep: true }],
    };

    it('normalizes the talkgroup order key drift on read', () => {
        const working = createAdminWorkingCopy(baseline);
        const tg = working.systems[0].talkgroups[0];

        assert.equal(tg.order, 3);
        assert.equal(tg['talkgroup'], undefined);
    });

    it('re-applies the order key on save serialization', () => {
        const working = createAdminWorkingCopy(baseline);
        working.systems[0].talkgroups[0].order = 9;

        const saved = serializeAdminConfig(mergeAdminConfig(baseline, working));
        const tg = saved.systems[0].talkgroups[0];

        assert.equal(tg.order, 9);
    });

    it('preserves unknown fields through the round-trip', () => {
        const working = createAdminWorkingCopy(baseline);
        working.options.branding = 'Y';

        const saved = serializeAdminConfig(mergeAdminConfig(baseline, working));

        assert.equal(saved.options.adminPassword, 'secret-hash');
        assert.equal(saved.options.secret, 'server-secret');
        assert.equal(saved.options.branding, 'Y');
        assert.equal(saved.systems[0].talkgroups[0].serverOnlyField, 42);
        assert.deepEqual(saved.unknownTopLevel, [{ keep: true }]);
        assert.equal(saved.version, '7.0.0-dev');
    });

    it('merge re-emits baseline keys missing from a partial working copy', () => {
        const partialWorking = { options: { branding: 'Z' } };

        const merged = mergeAdminConfig(baseline, partialWorking);

        assert.equal(merged.options.adminPassword, 'secret-hash');
        assert.equal(merged.options.secret, 'server-secret');
        assert.equal(merged.options.branding, 'Z');
        assert.deepEqual(merged.groups, baseline.groups);
    });

    // The unsaved-row mark is the client's own bookkeeping, keeping a row's
    // open panel and revealed messages across a rebuild. It must not ride out
    // on a save: the saved configuration is the product's data.
    it('drops the mark a client row carries through the whole-config round-trip', () => {
        const clientMark = Symbol('the key a client puts on a row it added');
        const working = createAdminWorkingCopy(baseline);

        working.systems[0].talkgroups[0][clientMark] = 1;
        working.groups = [...baseline.groups, { id: 0, label: 'New', order: 2, [clientMark]: 2 }];

        const saved = serializeAdminConfig(mergeAdminConfig(baseline, working));

        assert.deepEqual(saved.groups, [...baseline.groups, { id: 0, label: 'New', order: 2 }],
            'the row the operator added is saved with its configuration keys and no client key');
        assert.deepEqual(saved.systems[0].talkgroups[0], { id: 100, talkgroupRef: 100, label: 'TG1', name: 'N', order: 3, serverOnlyField: 42 },
            'a row the client marked keeps the configuration keys, the order key written under its saved name');
    });

    it('does not mutate the baseline', () => {
        const working = createAdminWorkingCopy(baseline);
        working.groups.push({ id: 2, label: 'G2' });

        assert.equal(baseline.groups.length, 1);
    });
});
