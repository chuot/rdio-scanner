// Scanner storage adapter: exact legacy keys (data-model.md §5).
// localStorage: rdio-scanner (legacy livefeed fallback), rdio-scanner-lfm-<id>,
// rdio-scanner-pin (handled by domain/pin against `local`).

import { STORAGE_KEY_LEGACY, STORAGE_KEY_LFM } from '../domain/livefeed-map.js';

export function createStorage({ localStorage }) {
  return {
    local: localStorage,

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
  };
}
