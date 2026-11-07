// Playback/listening queue.

export function enqueue(queue, call, options = {}) {
  return options.priority ? [call, ...queue] : [...queue, call];
}

export function shift(queue) {
  const [next, ...rest] = queue;
  return [next, rest];
}

export function clearQueue() {
  return [];
}

// keeps calls whose talkgroup, or one of its patches, is active.
// patches are resolved across all systems because talkgroup refs are
// globally unique; the legacy queue looked only in the call's own
// system and missed cross-system patches.
export function filterActive(queue, livefeedMap) {
  const isActive = (sys, tg) => livefeedMap?.[sys]?.[tg]?.active === true;

  return queue.filter((call) => {
    if (isActive(call.system, call.talkgroup)) return true;

    return (call.patches ?? []).some((patch) =>
      Object.values(livefeedMap ?? {}).some((tgMap) => tgMap?.[patch]?.active === true));
  });
}

// calls left to play, from the reference up to the newest row of the
// fetched page; the page's total and offset say what the filters left.
export function playbackQueueCount(list, callId) {
  const index = list.results.findIndex((call) => call.id === callId);
  if (index < 0) return 0;

  const offset = list.options?.offset ?? 0;

  const remaining = list.options?.sort === -1
    ? offset + index + 1
    : list.count - offset - index;

  return remaining > 0 ? remaining : 0;
}
