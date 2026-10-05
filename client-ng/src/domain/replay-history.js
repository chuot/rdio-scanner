// Replay history ring (legacy main.component.ts callHistory / replayOffset).

export const HISTORY_SIZE = 5;

export function recordHistory(history, call) {
  if (!call) return history;

  if (history.some((c) => c?.id === call.id)) return history;

  const next = [call, ...history];
  next.length = HISTORY_SIZE;
  return next;
}

// REPLAY presses walk the history within a 1s timer window:
// first press replays the current call (offset 0), then 1st, 2nd, … history item.
export function advanceReplayOffset(offset, historyLength) {
  return Math.min(historyLength, offset + 1);
}
