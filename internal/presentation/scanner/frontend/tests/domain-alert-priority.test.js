// tests for domain/alert-priority: the alert order group, tag, system, talkgroup, and the
// colour order the resolved payload gives the chain, group, tag, talkgroup
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { resolveAlert, resolveAlertSequence, resolveLedColor } from '../domain/alert-priority.js';

describe('domain/alert-priority — group → tag → system → talkgroup', () => {
    const config = {
        alerts: { A: [], B: [], C: [], D: [] },
        groupsData: [{ id: 1, label: 'G1', alert: 'A' }, { id: 2, label: 'G2' }],
        tagsData: [{ id: 3, label: 'T1', alert: 'B' }, { id: 4, label: 'T2' }],
    };

    it('group alert wins over everything else', () => {
        const call = {
            groupsData: [{ label: 'G2' }, { label: 'G1' }],
            tagData: { label: 'T1' },
            systemData: { alert: 'C' },
            talkgroupData: { alert: 'D' },
        };

        assert.equal(resolveAlert(call, config), 'A');
    });

    it('tag alert wins when no group has one', () => {
        const call = {
            groupsData: [{ label: 'G2' }],
            tagData: { label: 'T1' },
            systemData: { alert: 'C' },
            talkgroupData: { alert: 'D' },
        };

        assert.equal(resolveAlert(call, config), 'B');
    });

    it('system alert wins over talkgroup alert', () => {
        const call = {
            groupsData: [{ label: 'G2' }],
            tagData: { label: 'T2' },
            systemData: { alert: 'C' },
            talkgroupData: { alert: 'D' },
        };

        assert.equal(resolveAlert(call, config), 'C');
    });

    it('talkgroup alert is last in priority', () => {
        const call = {
            groupsData: [{ label: 'G2' }],
            tagData: { label: 'T2' },
            systemData: {},
            talkgroupData: { alert: 'D' },
        };

        assert.equal(resolveAlert(call, config), 'D');
    });

    it('no alert when nothing defines one', () => {
        assert.equal(resolveAlert({ groupsData: [], tagData: undefined }, config), undefined);
    });
});

describe('domain/alert-priority — LED color resolution: group → tag → talkgroup', () => {
    const colors = ['blue', 'cyan', 'green', 'magenta', 'orange', 'red', 'white', 'yellow'];

    it('group led wins, then tag, then talkgroup', () => {
        assert.equal(
            resolveLedColor({
                groupsData: [{ led: 'red' }],
                tagData: { led: 'blue' },
                systemData: { led: 'cyan' },
                talkgroupData: { led: 'orange' },
            }, colors),
            'red',
        );

        assert.equal(
            resolveLedColor({ groupsData: [], tagData: { led: 'blue' }, systemData: { led: 'cyan' } }, colors),
            'blue',
        );

        assert.equal(
            resolveLedColor({
                groupsData: [],
                systemData: { led: 'cyan' },
                talkgroupData: { led: 'cyan' },
            }, colors),
            'cyan',
        );

        assert.equal(
            resolveLedColor({ groupsData: [], talkgroupData: { led: 'orange' } }, colors),
            'orange',
        );
    });

    it('falls back to green for missing or invalid colors', () => {
        assert.equal(resolveLedColor({ groupsData: [] }, colors), 'green');
        assert.equal(resolveLedColor({ talkgroupData: { led: 'pink' } }, colors), 'green');
    });
});

describe('domain/alert-priority — the colour the server resolved into the payload', () => {
    const colors = ['blue', 'cyan', 'green', 'magenta', 'orange', 'red', 'white', 'yellow'];

    // Every call here is the shape a listener receives from the server: a system entry
    // carrying a colour, a talkgroup entry carrying the colour the server resolved for that
    // talkgroup, and group and tag entries carrying what the administrator stored.
    const rows = [
        {
            name: 'a coloured group answers although its system entry carries the default',
            call: {
                groupsData: [{ id: 1, label: 'Fire', led: 'red' }],
                systemData: { id: 10, label: 'Metro', led: 'green' },
                talkgroupData: { id: 100, label: 'Fire 1', led: 'red' },
            },
            want: 'red',
        },
        {
            name: 'a talkgroup storing a colour answers under a system that stores none',
            call: {
                groupsData: [{ id: 1, label: 'Fire' }],
                tagData: { id: 20, label: 'Dispatch' },
                systemData: { id: 10, label: 'Metro', led: 'green' },
                talkgroupData: { id: 100, label: 'Fire 1', led: 'red' },
            },
            want: 'red',
        },
        {
            name: 'a coloured system answers through the talkgroup entry it folds into',
            call: {
                groupsData: [],
                tagData: { id: 20, label: 'Dispatch' },
                systemData: { id: 10, label: 'Metro', led: 'cyan' },
                talkgroupData: { id: 100, label: 'Fire 1', led: 'cyan' },
            },
            want: 'cyan',
        },
        {
            name: 'a coloured tag answers under a system that stores none',
            call: {
                groupsData: [],
                tagData: { id: 20, label: 'Dispatch', led: 'blue' },
                systemData: { id: 10, label: 'Metro', led: 'green' },
                talkgroupData: { id: 100, label: 'Fire 1', led: 'blue' },
            },
            want: 'blue',
        },
        {
            name: 'a coloured group outranks a coloured system',
            call: {
                groupsData: [{ id: 1, label: 'Fire', led: 'red' }, { id: 2, label: 'Ground' }],
                systemData: { id: 10, label: 'Metro', led: 'magenta' },
                talkgroupData: { id: 100, label: 'Fire 1', led: 'red' },
            },
            want: 'red',
        },
        {
            name: 'the default answers when no level stores a colour',
            call: {
                groupsData: [{ id: 1, label: 'Fire' }],
                tagData: { id: 20, label: 'Dispatch' },
                systemData: { id: 10, label: 'Metro', led: 'green' },
                talkgroupData: { id: 100, label: 'Fire 1', led: 'green' },
            },
            want: 'green',
        },
        {
            name: 'a group storing a name outside the eight answers green, as it does today',
            call: {
                groupsData: [{ id: 1, label: 'Fire', led: 'pink' }],
                systemData: { id: 10, label: 'Metro', led: 'green' },
                talkgroupData: { id: 100, label: 'Fire 1', led: 'red' },
            },
            want: 'green',
        },
        {
            name: 'a tag storing a name outside the eight answers green, as it does today',
            call: {
                groupsData: [{ id: 1, label: 'Fire' }],
                tagData: { id: 20, label: 'Dispatch', led: 'pink' },
                systemData: { id: 10, label: 'Metro', led: 'green' },
                talkgroupData: { id: 100, label: 'Fire 1', led: 'red' },
            },
            want: 'green',
        },
        {
            name: 'a system storing a name outside the eight lets its talkgroup answer',
            call: {
                groupsData: [],
                tagData: { id: 20, label: 'Dispatch' },
                systemData: { id: 10, label: 'Metro', led: 'green' },
                talkgroupData: { id: 100, label: 'Fire 1', led: 'red' },
            },
            want: 'red',
        },
    ];

    for (const row of rows) {
        it(row.name, () => {
            assert.equal(resolveLedColor(row.call, colors), row.want);
        });
    }
});

describe('domain/alert-priority — the tone sequence the call carries', () => {
    const tone = [{ begin: 0, end: 0.05, frequency: 3000, type: 'square' }];

    const config = {
        alerts: { group: [tone[0]], tag: tone, system: tone, talkgroup: tone },
        groupsData: [{ id: 1, label: 'G1', alert: 'group' }, { id: 2, label: 'G2' }],
        tagsData: [{ id: 3, label: 'T1', alert: 'tag' }, { id: 4, label: 'T2' }],
    };

    it('the tone of a group wins over every other level', () => {
        const call = {
            groupsData: [{ label: 'G2' }, { label: 'G1' }],
            tagData: { label: 'T1' },
            systemData: { alert: 'system' },
            talkgroupData: { alert: 'talkgroup' },
        };

        assert.deepEqual(resolveAlertSequence(call, config), config.alerts.group);
    });

    it('the tone is read from the group listing the configuration carries, not from the call', () => {
        // The call carries the group's label only; the listing is where its alert lives.
        const call = { groupsData: [{ label: 'G1' }] };

        assert.deepEqual(resolveAlertSequence(call, config), config.alerts.group);
    });

    it('the tone of a tag wins when no group carries one', () => {
        const call = {
            groupsData: [{ label: 'G2' }],
            tagData: { label: 'T1' },
            systemData: { alert: 'system' },
            talkgroupData: { alert: 'talkgroup' },
        };

        assert.deepEqual(resolveAlertSequence(call, config), config.alerts.tag);
    });

    it('the tone of a system wins over the tone of a talkgroup', () => {
        const call = {
            groupsData: [{ label: 'G2' }],
            tagData: { label: 'T2' },
            systemData: { alert: 'system' },
            talkgroupData: { alert: 'talkgroup' },
        };

        assert.deepEqual(resolveAlertSequence(call, config), config.alerts.system);
    });

    it('the tone of a talkgroup is last in the assignment order', () => {
        const call = {
            groupsData: [{ label: 'G2' }],
            tagData: { label: 'T2' },
            systemData: {},
            talkgroupData: { alert: 'talkgroup' },
        };

        assert.deepEqual(resolveAlertSequence(call, config), config.alerts.talkgroup);
    });

    it('no tone when no level carries an alert', () => {
        const call = {
            groupsData: [{ label: 'G2' }],
            tagData: { label: 'T2' },
            systemData: {},
            talkgroupData: {},
        };

        assert.equal(resolveAlertSequence(call, config), undefined);
    });

    it('no tone when the configured name is not one the server carries', () => {
        const call = { talkgroupData: { alert: 'alert10' } };

        assert.equal(resolveAlertSequence(call, config), undefined);
    });

    it('no tone when the table entry is not a sequence', () => {
        const table = {
            ...config,
            alerts: { ...config.alerts, talkgroup: [] },
        };

        assert.equal(resolveAlertSequence({ talkgroupData: { alert: 'talkgroup' } }, table), undefined);

        assert.equal(
            resolveAlertSequence({ talkgroupData: { alert: 'talkgroup' } }, {
                ...config,
                alerts: { ...config.alerts, talkgroup: null },
            }),
            undefined,
        );

        assert.equal(
            resolveAlertSequence({ talkgroupData: { alert: 'talkgroup' } }, {
                ...config,
                alerts: { ...config.alerts, talkgroup: 'square' },
            }),
            undefined,
        );
    });

    it('no tone when the call carries no levels at all', () => {
        assert.equal(resolveAlertSequence(undefined, config), undefined);
        assert.equal(resolveAlertSequence({}, config), undefined);
        assert.equal(resolveAlertSequence({}, undefined), undefined);
    });
});
