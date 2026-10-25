// tests for the admin application context: it carries the admin slice and nothing else
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { createAdminContext } from '../application/admin-context.js';

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

    return { localStorage: make(), sessionStorage: make() };
}

function createWiring() {
    const fakes = createFakes();

    return {
        ...fakes,
        context: createAdminContext({
            location: new URL('http://localhost/admin/'),
            localStorage: fakes.localStorage,
            sessionStorage: fakes.sessionStorage,
        }),
    };
}

describe('application/admin-context — the storage port', () => {
    it('keeps the admin token in sessionStorage and never touches localStorage', () => {
        const { localStorage, sessionStorage, context } = createWiring();

        assert.equal(context.storage.getAdminToken(), '');

        context.storage.setAdminToken('jwt-token');

        assert.equal(sessionStorage.getItem('rdio-scanner-admin-token'), 'jwt-token');
        assert.equal(context.storage.getAdminToken(), 'jwt-token');
        assert.equal(localStorage.store.size, 0);
    });

    it('carries no listener storage behavior into the admin composition root', () => {
        const { context } = createWiring();

        for (const name of ['local', 'session', 'readLivefeedMap', 'writeLivefeedMap', 'clearAll']) {
            assert.equal(name in context.storage, false, `the admin storage must not carry ${name}`);
        }
    });
});
