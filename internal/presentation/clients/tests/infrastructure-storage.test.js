import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { createStorage } from '../shared/infrastructure/storage.js';
import { clearPin, readPin, savePin } from '../src/domain/pin.js';

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
    const sessionStorage = make();

    return { localStorage, sessionStorage, createStorage: () => createStorage({ localStorage, sessionStorage }) };
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

describe('infrastructure/storage — admin token session', () => {
    it('keeps the admin token in sessionStorage under the legacy key', () => {
        const { sessionStorage, createStorage } = createFakes();
        const storage = createStorage();

        assert.equal(storage.getAdminToken(), '');

        storage.setAdminToken('jwt-token');

        assert.equal(sessionStorage.getItem('rdio-scanner-admin-token'), 'jwt-token');
        assert.equal(storage.getAdminToken(), 'jwt-token');

        storage.clearAdminToken();

        assert.equal(storage.getAdminToken(), '');
    });
});

describe('infrastructure/storage — /reset', () => {
    it('clearAll wipes localStorage entirely', () => {
        const { localStorage, sessionStorage, createStorage } = createFakes();
        const storage = createStorage();

        storage.writeLivefeedMap('default', { 10: { 100: true } });
        savePin(storage.local, 'x');
        storage.setAdminToken('jwt');

        storage.clearAll();

        assert.equal(localStorage.store.size, 0);
        assert.notEqual(sessionStorage.getItem('rdio-scanner-admin-token'), null);
    });
});
