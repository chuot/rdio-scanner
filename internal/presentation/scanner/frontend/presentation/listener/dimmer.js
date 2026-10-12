// The display dimmer: the display lights when a call arrives
// or the listener presses a control, and dims again DIMMER_DELAY_MS after that.
// The delay is a domain constant; the retired dimmer delay option is gone. The
// timers are injected so the timing can be observed with a fake clock.

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
