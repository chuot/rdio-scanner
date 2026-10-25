// The application store's slice subscriptions and notification rules.
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { createStore } from '../application/store.js';

describe('application/store — slice subscriptions', () => {
    it('notifies subscribers of the slice that changed with its value', () => {
        const store = createStore({ config: null, map: null });
        const received = [];

        store.subscribe('map', (event) => received.push(event.map));

        store.setState({ map: { 10: { 100: { active: true } } } });

        assert.deepEqual(received, [{ 10: { 100: { active: true } } }]);
    });

    it('does not notify subscribers of untouched slices', () => {
        const store = createStore({ config: null, map: null });
        let calls = 0;

        store.subscribe('config', () => calls++);

        store.setState({ map: {} });

        assert.equal(calls, 0);
    });

    it('supports subscribing to several slices at once', () => {
        const store = createStore({ categories: null, map: null });
        const received = [];

        store.subscribe(['categories', 'map'], (event) => received.push(event));

        store.setState({ categories: [{ label: 'Fire' }], map: {} });

        assert.deepEqual(received, [{ categories: [{ label: 'Fire' }], map: {} }]);
    });

    it('unsubscribe stops delivery', () => {
        const store = createStore({ map: null });
        let calls = 0;

        const unsubscribe = store.subscribe('map', () => calls++);

        store.setState({ map: { a: 1 } });
        unsubscribe();
        store.setState({ map: { a: 2 } });

        assert.equal(calls, 1);
    });
});

describe('application/store — structural diffing', () => {
    it('skips notification when the new slice is deep-equal to the current one', () => {
        const store = createStore({ map: { 10: { 100: { active: true } } } });
        let calls = 0;

        store.subscribe('map', () => calls++);

        store.setState({ map: { 10: { 100: { active: true } } } });

        assert.equal(calls, 0);
    });

    it('notifies when a nested value differs', () => {
        const store = createStore({ map: { 10: { 100: { active: true } } } });
        let calls = 0;

        store.subscribe('map', () => calls++);

        store.setState({ map: { 10: { 100: { active: false } } } });

        assert.equal(calls, 1);
    });

    it('getState returns the merged state', () => {
        const store = createStore({ config: null });

        store.setState({ config: { branding: 'X' } });

        assert.equal(store.getState().config.branding, 'X');
    });
});

describe('application/store — event bus parity with legacy RdioScannerEvent', () => {
    it('a single setState can carry the whole legacy event object', () => {
        const store = createStore({});
        const received = [];

        store.subscribe(['categories', 'holdSys', 'holdTg', 'map', 'queue'], (event) => received.push(event));

        store.setState({ categories: [], holdSys: false, holdTg: false, map: {}, queue: 0 });

        assert.equal(received.length, 1);
        assert.deepEqual(Object.keys(received[0]).sort(), ['categories', 'holdSys', 'holdTg', 'map', 'queue']);
    });

    it('late subscribers do not replay past events', () => {
        const store = createStore({ map: null });

        store.setState({ map: { a: 1 } });

        let calls = 0;
        store.subscribe('map', () => calls++);

        assert.equal(calls, 0);
    });
});

describe('application/store — notification snapshot', () => {
    // The notification walks the subscriptions as they were at the start: a
    // component moving its own binding mid-notify must not be entered by the
    // subscription it took there, nor by one it released.
    it('does not enter a subscription taken while the notification is running', () => {
        const store = createStore({ map: null, panel: null });
        const entered = [];

        store.subscribe('map', () => {
            store.subscribe('panel', () => entered.push('taken mid-notification'));
        });

        store.setState({ map: { a: 1 }, panel: { b: 2 } });

        assert.deepEqual(entered, [], 'the notification walks the subscriptions it started with');

        store.setState({ panel: { b: 3 } });

        assert.deepEqual(entered, ['taken mid-notification'], 'the new subscription is entered from the next notification on');
    });

    it('does not enter a subscription released while the notification is running', () => {
        const store = createStore({ map: null, panel: null });
        const entered = [];

        // The subscriber that releases the binding is declared first, so the
        // notification reaches the released subscription only by walking the
        // subscriptions as they are mid-notification.
        store.subscribe('map', () => unsubscribePanel());

        const unsubscribePanel = store.subscribe('panel', () => entered.push('released mid-notification'));

        store.setState({ map: { a: 1 }, panel: { b: 2 } });

        assert.deepEqual(entered, [], 'a subscription that is gone is not entered after it is gone');
    });
});
