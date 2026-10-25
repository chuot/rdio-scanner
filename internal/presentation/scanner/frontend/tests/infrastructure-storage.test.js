// tests for infrastructure/storage: the livefeed map kept between sessions
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { createStorage } from '../infrastructure/storage.js';
import { clearPin, readPin, savePin } from '../domain/pin.js';

function createFakes() {
    const make = () => {
        const store = new Map();

        return {
            store,
            getItem: (key) => (store.has(key) ? store.get(key) : null),
            setItem: (key, value) => store.set(key, String(value)),
            removeItem: (key) => store.delete(key),
            clear: () => store.clear(),
        };
    };

    const localStorage = make();

    return { localStorage, createStorage: () => createStorage({ localStorage }) };
}

describe('infrastructure/storage — livefeed map', () => {
    it('reads the instance-namespaced key', () => {
        const { localStorage, createStorage } = createFakes();

        localStorage.setItem('rdio-scanner-lfm-profile1', JSON.stringify({ 10: { 100: true } }));

        assert.deepEqual(createStorage().readLivefeedMap('profile1'), { 10: { 100: true } });
    });

    it('falls back to the legacy rdio-scanner key when the namespaced key is absent', () => {
        const { localStorage, createStorage } = createFakes();

        localStorage.setItem('rdio-scanner', JSON.stringify({ 10: { 100: false } }));

        assert.deepEqual(createStorage().readLivefeedMap('default'), { 10: { 100: false } });
    });

    it('returns an empty map when nothing is stored or the store is corrupt', () => {
        const { localStorage, createStorage } = createFakes();

        assert.deepEqual(createStorage().readLivefeedMap('default'), {});

        localStorage.setItem('rdio-scanner-lfm-default', '{oops');

        assert.deepEqual(createStorage().readLivefeedMap('default'), {});
    });

    it('writes the namespaced key', () => {
        const { localStorage, createStorage } = createFakes();

        createStorage().writeLivefeedMap('default', { 10: { 100: true } });

        assert.equal(localStorage.getItem('rdio-scanner-lfm-default'), JSON.stringify({ 10: { 100: true } }));
    });
});

describe('infrastructure/storage — pin', () => {
    it('persists the pin base64 under rdio-scanner-pin via the domain helpers', () => {
        const { localStorage, createStorage } = createFakes();
        const storage = createStorage();

        savePin(storage.local, 'hunter2');

        assert.equal(localStorage.getItem('rdio-scanner-pin'), btoa('hunter2'));
        assert.equal(readPin(storage.local), 'hunter2');

        clearPin(storage.local);

        assert.equal(readPin(storage.local), undefined);
    });
});

describe('infrastructure/storage — /reset', () => {
    it('clearAll wipes localStorage entirely', () => {
        const { localStorage, createStorage } = createFakes();
        const storage = createStorage();

        storage.writeLivefeedMap('default', { 10: { 100: true } });
        savePin(storage.local, 'x');

        storage.clearAll();

        assert.equal(localStorage.store.size, 0);
    });

    it('reaches no session storage at all', () => {
        const storage = createFakes().createStorage();

        for (const name of ['session', 'getAdminToken', 'setAdminToken', 'clearAdminToken']) {
            assert.equal(name in storage, false, `the listener storage must not carry ${name}`);
        }
    });
});
