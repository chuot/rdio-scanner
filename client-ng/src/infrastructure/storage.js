// Browser storage adapter: exact legacy keys (data-model.md §5).
// localStorage: rdio-scanner (legacy livefeed fallback), rdio-scanner-lfm-<id>,
// rdio-scanner-pin (handled by domain/pin against `local`).
// sessionStorage: rdio-scanner-admin-token.

import { STORAGE_KEY_LEGACY, STORAGE_KEY_LFM } from '../domain/livefeed-map.js';

export const ADMIN_TOKEN_KEY = 'rdio-scanner-admin-token';

export function createStorage({ localStorage, sessionStorage }) {
  return {
    local: localStorage,
    session: sessionStorage,

    readLivefeedMap(instanceId) {
      try {
        let store = localStorage.getItem(`${STORAGE_KEY_LFM}-${instanceId}`);

        if (store === null) {
          store = localStorage.getItem(STORAGE_KEY_LEGACY);
        }

        return (store !== null ? JSON.parse(store) : {}) ?? {};
      } catch {
        return {};
      }
    },

    writeLivefeedMap(instanceId, encoded) {
      localStorage.setItem(`${STORAGE_KEY_LFM}-${instanceId}`, JSON.stringify(encoded));
    },

    clearAll() {
      localStorage.clear();
    },

    getAdminToken() {
      return sessionStorage.getItem(ADMIN_TOKEN_KEY) || '';
    },

    setAdminToken(token) {
      sessionStorage.setItem(ADMIN_TOKEN_KEY, token);
    },

    clearAdminToken() {
      sessionStorage.removeItem(ADMIN_TOKEN_KEY);
    },
  };
}
