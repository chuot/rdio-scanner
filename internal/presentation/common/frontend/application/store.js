// Application store: slice-based subscriptions with structural diffing,
// replacing the legacy RdioScannerService EventEmitter.
// see the store-slice-subscriptions maintainer note.

export function createStore(initialState = {}) {
  const state = { ...initialState };
  const subscriptions = new Set();

  return {
    getState() {
      return state;
    },

    setState(partial) {
      const changed = {};

      for (const [key, value] of Object.entries(partial)) {
        if (!deepEqual(state[key], value)) {
          state[key] = value;
          changed[key] = value;
        }
      }

      if (Object.keys(changed).length === 0) return;

      // Notify a snapshot of the subscriptions as this notification started,
      // skipping the ones released on the way:
      // see the store-slice-subscriptions maintainer note.
      for (const subscription of [...subscriptions]) {
        if (!subscriptions.has(subscription)) continue;

        const event = {};
        let touched = false;

        for (const key of subscription.keys) {
          if (key in changed) {
            event[key] = changed[key];
            touched = true;
          }
        }

        if (touched) subscription.callback(event);
      }
    },

    subscribe(keys, callback) {
      const declared = Array.isArray(keys) ? keys : [keys];
      const subscription = { keys: declared, callback };

      subscriptions.add(subscription);

      return () => subscriptions.delete(subscription);
    },
  };
}

export function deepEqual(a, b) {
  if (a === b) return true;

  if (a === null || b === null || typeof a !== 'object' || typeof b !== 'object') return false;

  if (Array.isArray(a) !== Array.isArray(b)) return false;

  const aKeys = Object.keys(a);
  const bKeys = Object.keys(b);

  if (aKeys.length !== bKeys.length) return false;

  return aKeys.every((key) => key in b && deepEqual(a[key], b[key]));
}
