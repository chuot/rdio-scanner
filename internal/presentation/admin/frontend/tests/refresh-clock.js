// a named interval is taken off the clock; its re-arming chain cannot keep the test alive
// holding it lets a test fire the poll instead of waiting on the wall clock.
export function holdRefreshInterval(delay) {
  const realSetTimeout = globalThis.setTimeout;
  const realClearTimeout = globalThis.clearTimeout;

  const waiting = new Map();

  let nextId = 1_000_000;

  globalThis.setTimeout = (fn, ms, ...rest) => {
    if (ms !== delay) return realSetTimeout(fn, ms, ...rest);

    const id = nextId++;

    waiting.set(id, fn);

    return id;
  };

  globalThis.clearTimeout = (id) => {
    if (waiting.delete(id)) return undefined;

    return realClearTimeout(id);
  };

  return {
    // How many polls the page is waiting for. A schedule that started twice would show two.
    waitingCount: () => waiting.size,

    // runs every waiting tick once, in order; the count is the number of polls
    fire: () => {
      const due = [...waiting.values()];

      waiting.clear();

      for (const fn of due) fn();

      return due.length;
    },

    // gives the clock back, dropping held intervals so no schedule restarts live
    release: () => {
      globalThis.setTimeout = realSetTimeout;
      globalThis.clearTimeout = realClearTimeout;

      waiting.clear();
    },
  };
}
