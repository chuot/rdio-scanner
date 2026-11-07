// the log view's auto-refresh decision: no clock, no DOM; the element hands in the
// poll and the timer.

export const LOG_REFRESH_DELAY = 10000;

export function createRefreshSchedule({
  poll,
  delay = LOG_REFRESH_DELAY,
  isBusy = () => false,
  setTimer = (fn, ms) => setTimeout(fn, ms),
  clearTimer = (id) => clearTimeout(id),
}) {
  let timer = null;

  function step() {
    // the next poll is scheduled before this one runs: a failed poll does not end the
    // refresh.
    timer = setTimer(step, delay);

    if (isBusy()) return;

    poll();
  }

  return {
    start() {
      if (timer !== null) return;

      timer = setTimer(step, delay);
    },

    stop() {
      if (timer === null) return;

      clearTimer(timer);

      timer = null;
    },

    isRunning: () => timer !== null,
  };
}
