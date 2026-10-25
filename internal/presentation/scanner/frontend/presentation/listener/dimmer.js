// display dimmer: lights the display when a call arrives or a control is pressed, and dims it
// again DIMMER_DELAY_MS later; the delay is a domain constant, and timers are injected so a
// fake clock can observe the timing.

export const DIMMER_DELAY_MS = 5000;

export function createDimmer({
  setTimeoutImpl = globalThis.setTimeout,
  clearTimeoutImpl = globalThis.clearTimeout,
  setDimmed,
}) {
  let timer = null;

  return {
    arm() {
      setDimmed(false);

      clearTimeoutImpl(timer);

      timer = setTimeoutImpl(() => setDimmed(true), DIMMER_DELAY_MS);
    },

    stop() {
      clearTimeoutImpl(timer);

      timer = null;
    },
  };
}
