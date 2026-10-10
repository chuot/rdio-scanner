// The log view's auto-refresh: what the toggle decides, with no clock and no DOM in it. The element hands
// in the poll and the timer, so that starting, stopping, and the two things that matter about them — that
// starting twice does not double the polling, and that a refresh does not stack a request on top of one
// already in flight — can be seen without waiting for anything.

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
    // The next poll is scheduled before this one runs, so a poll that fails does not quietly end the
    // refresh: an operator who turned auto-refresh on asked to be kept current, not to be kept current
    // until something went wrong.
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
