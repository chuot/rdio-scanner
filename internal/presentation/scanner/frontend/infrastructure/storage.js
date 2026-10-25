// Scanner storage adapter: exact legacy keys, rdio-scanner as the
// livefeed fallback, rdio-scanner-lfm-<id> per listener, and
// rdio-scanner-pin handled by domain/pin.

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
