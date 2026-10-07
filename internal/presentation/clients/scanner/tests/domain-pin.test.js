import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
    PIN_STORAGE_KEY,
    clearPin,
    decodePin,
    encodePin,
    initialAuthState,
    nextAuthState,
    readPin,
    savePin,
} from '../domain/pin.js';

function createStorage() {
    const store = new Map();

    return {
        getItem: (key) => (store.has(key) ? store.get(key) : null),
        setItem: (key, value) => store.set(key, String(value)),
        removeItem: (key) => store.delete(key),
    };
}

describe('domain/pin — storage', () => {
    it('uses the legacy storage key', () => {
        assert.equal(PIN_STORAGE_KEY, 'rdio-scanner-pin');
    });

    it('stores the pin base64-encoded, like legacy window.btoa/atob', () => {
        const storage = createStorage();

        savePin(storage, 'hunter2');

        assert.equal(storage.getItem('rdio-scanner-pin'), btoa('hunter2'));
        assert.equal(readPin(storage), 'hunter2');
    });

    it('readPin returns undefined when nothing is stored', () => {
        assert.equal(readPin(createStorage()), undefined);
    });

    it('clearPin removes the stored pin', () => {
        const storage = createStorage();

        savePin(storage, 'x');
        clearPin(storage);

        assert.equal(readPin(storage), undefined);
    });

    it('encodePin/decodePin round-trip', () => {
        assert.equal(decodePin(encodePin('pâss123')), 'pâss123');
    });
});

describe('domain/pin — auth state machine', () => {
    it('starts unauthenticated and shows the pin prompt when the server asks', () => {
        const state = initialAuthState();

        assert.equal(state.status, 'anonymous');
        assert.equal(nextAuthState(state, { type: 'pin-prompt' }).status, 'pin-required');
    });

    it('submitting a pin moves to authenticating', () => {
        const state = nextAuthState(initialAuthState(), { type: 'pin-prompt' });

        assert.equal(nextAuthState(state, { type: 'pin-submit' }).status, 'authenticating');
    });

    it('a repeated pin prompt after submit means the pin was rejected', () => {
        let state = nextAuthState(initialAuthState(), { type: 'pin-submit' });
        state = nextAuthState(state, { type: 'pin-prompt' });

        assert.equal(state.status, 'pin-required');
        assert.equal(state.denied, true);
    });

    it('receiving the config authenticates the listener', () => {
        const state = nextAuthState(initialAuthState(), { type: 'config' });

        assert.equal(state.status, 'authenticated');
    });

    it('MAX moves to too-many and XPR to expired', () => {
        assert.equal(nextAuthState(initialAuthState(), { type: 'too-many' }).status, 'too-many');
        assert.equal(nextAuthState(initialAuthState(), { type: 'expired' }).status, 'expired');
    });

    it('websocket loss returns to the anonymous baseline', () => {
        let state = nextAuthState(initialAuthState(), { type: 'config' });
        state = nextAuthState(state, { type: 'disconnected' });

        assert.equal(state.status, 'anonymous');
    });
});
