// A section's own poll interval, taken off the clock.
//
// A refresh schedule re-arms itself: the next poll is scheduled before the current one runs, so a schedule
// that is left running is a chain of timers that never ends, and a test process waits for every timer it
// was given before it exits. Holding the interval is also what lets a test say "the interval arrived"
// instead of waiting for it, which is the difference between a test that is fast and repeatable and a test
// that depends on the wall clock.
//
// Only the interval named is held. Every other timer the page or the test runner sets — a socket reconnect,
// a snackbar, the runner's own — keeps running on the real clock, so holding one does not reach into the
// others.
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

    // The interval arriving: everything waiting is run, once, in the order it was scheduled. The count is
    // what the page polled, so a test can say a paused page polled nothing.
    fire: () => {
      const due = [...waiting.values()];

      waiting.clear();

      for (const fn of due) fn();

      return due.length;
    },

    // Putting the clock back. Every held interval is dropped, because a test that hands the clock back
    // while its own schedule is still waiting would leave that schedule running on the real clock.
    release: () => {
      globalThis.setTimeout = realSetTimeout;
      globalThis.clearTimeout = realClearTimeout;

      waiting.clear();
    },
  };
}
