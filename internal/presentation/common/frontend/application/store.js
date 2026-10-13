// Application store: slice-based subscriptions with structural diffing,
// replacing the legacy RdioScannerService EventEmitter<RdioScannerEvent>.
// Subscribers declare the slices they care about; a setState only notifies
// subscribers whose declared slices actually changed (deep-compared). A
// notification walks the subscriptions it started with, so a subscriber that
// moves its own binding inside a notification is safe to do so.

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

      // Notify the subscriptions as they were when this notification started, and
      // skip the ones released on the way: a component that moves its own binding
      // while it is being notified must not be entered a second time by the
      // subscription it took inside the notification, and a subscription released
      // there must not be entered after it is gone.
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
