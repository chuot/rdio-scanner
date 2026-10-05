// Playback/listening queue (legacy rdio-scanner.service.ts queue/cleanQueue).

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

// Keeps calls whose talkgroup — or one of its patches — is active.
// Deviation from legacy: patches are resolved across all systems because
// talkgroupRefs are globally unique (legacy looked only in the call's own
// system and missed cross-system patches — decision-legacy-bug-fixes.md).
export function filterActive(queue, livefeedMap) {
  const isActive = (sys, tg) => livefeedMap?.[sys]?.[tg]?.active === true;

  return queue.filter((call) => {
    if (isActive(call.system, call.talkgroup)) return true;

    return (call.patches ?? []).some((patch) =>
      Object.values(livefeedMap ?? {}).some((tgMap) => tgMap?.[patch]?.active === true));
  });
}

// Remaining calls relative to a reference id inside a fetched server page
// (legacy queueCount): descending sort → offset + index; ascending → count − offset − index − 1.
export function playbackQueueCount(list, callId) {
  const index = list.results.findIndex((call) => call.id === callId);
  if (index < 0) return 0;

  if (list.options.sort === -1) {
    return list.options.offset + index;
  }

  return list.count - list.options.offset - index - 1;
}
