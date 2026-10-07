import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { resolveAlert, resolveLedColor } from '../domain/alert-priority.js';

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

describe('domain/alert-priority — LED color resolution', () => {
    const colors = ['blue', 'cyan', 'green', 'magenta', 'orange', 'red', 'white', 'yellow'];

    it('group led wins, then tag, system, talkgroup', () => {
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
            resolveLedColor({ groupsData: [], systemData: { led: 'cyan' }, talkgroupData: { led: 'orange' } }, colors),
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
