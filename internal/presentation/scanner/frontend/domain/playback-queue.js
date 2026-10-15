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
// system and missed cross-system patches).
export function filterActive(queue, livefeedMap) {
  const isActive = (sys, tg) => livefeedMap?.[sys]?.[tg]?.active === true;

  return queue.filter((call) => {
    if (isActive(call.system, call.talkgroup)) return true;

    return (call.patches ?? []).some((patch) =>
      Object.values(livefeedMap ?? {}).some((tgMap) => tgMap?.[patch]?.active === true));
  });
}

// Remaining calls of a searched playback, relative to a reference id inside a fetched server
// page: the calls that will play from the reference up to the newest of the result set, the
// reference among them. Playback walks toward the newest call in both sort orders — a descending
// page starts at its oldest row and walks back up to the newest, an ascending page walks forward
// to it — so the calls left to play are the ones between the reference and the newest end of the
// page, and the page's own total and offset say how many the search filters left there.
export function playbackQueueCount(list, callId) {
  const index = list.results.findIndex((call) => call.id === callId);
  if (index < 0) return 0;

  const offset = list.options?.offset ?? 0;

  const remaining = list.options?.sort === -1
    ? offset + index + 1
    : list.count - offset - index;

  return remaining > 0 ? remaining : 0;
}
