// tests for infrastructure/token-storage: where the admin token lives
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { ADMIN_TOKEN_KEY, createTokenStorage } from '../infrastructure/token-storage.js';

function createFakes() {
    const sessionStorage = new Map();

    return {
        sessionStorage,
        createTokenStorage: () =>
            createTokenStorage({
                sessionStorage: {
                    getItem: (key) => (sessionStorage.has(key) ? sessionStorage.get(key) : null),
                    setItem: (key, value) => sessionStorage.set(key, String(value)),
                    removeItem: (key) => sessionStorage.delete(key),
                },
            }),
    };
}

describe('infrastructure/token-storage — admin token session', () => {
    it('keeps the admin token in sessionStorage under the legacy key', () => {
        const { sessionStorage, createTokenStorage } = createFakes();
        const storage = createTokenStorage();

        assert.equal(storage.getAdminToken(), '');

        storage.setAdminToken('jwt-token');

        assert.equal(sessionStorage.get(ADMIN_TOKEN_KEY), 'jwt-token');
        assert.equal(storage.getAdminToken(), 'jwt-token');

        storage.clearAdminToken();

        assert.equal(storage.getAdminToken(), '');
        assert.equal(sessionStorage.has(ADMIN_TOKEN_KEY), false);
    });

    it('names the key the legacy client used', () => {
        assert.equal(ADMIN_TOKEN_KEY, 'rdio-scanner-admin-token');
    });
});
