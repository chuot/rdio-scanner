// Livefeed map: per-listener talkgroup activation state.
// Internal shape: { [systemRef]: { [talkgroupRef]: { active, minutes? } } }
// Wire/storage shape (LFM message, localStorage): { [systemRef]: { [talkgroupRef]: boolean } }
// All operations are immutable: they return a new map, the input stays intact
// (the prior-to-hold snapshot must survive hold toggling).

export const STORAGE_KEY_LEGACY = 'rdio-scanner';
export const STORAGE_KEY_LFM = 'rdio-scanner-lfm';

export function storageKey(instanceId) {
  return `${STORAGE_KEY_LFM}-${instanceId}`;
}

export function legacyStorageKey() {
  return STORAGE_KEY_LEGACY;
}

export function encodeMap(map) {
  const encoded = {};
  for (const [sys, tgMap] of Object.entries(map)) {
    encoded[sys] = {};
    for (const [tg, lfm] of Object.entries(tgMap)) encoded[sys][tg] = lfm.active;
  }
  return encoded;
}

export function decodeMap(encoded) {
  const map = {};
  for (const [sys, tgMap] of Object.entries(encoded ?? {})) {
    map[sys] = {};
    for (const [tg, active] of Object.entries(tgMap)) map[sys][tg] = { active };
  }
  return map;
}

function cloneEntry(lfm) {
  const entry = { active: lfm.active };
  if (lfm.minutes !== undefined) entry.minutes = lfm.minutes;
  return entry;
}

export function cloneMap(map) {
  const next = {};
  for (const [sys, tgMap] of Object.entries(map)) {
    next[sys] = {};
    for (const [tg, lfm] of Object.entries(tgMap)) next[sys][tg] = cloneEntry(lfm);
  }
  return next;
}

// options: { status?: boolean, minutes?: number } — legacy avoid() semantics:
// status forces the new active state, minutes arms a timed avoid.
export function toggleTalkgroup(map, systemRef, talkgroupRef, options = {}) {
  const next = cloneMap(map);
  const lfm = next?.[systemRef]?.[talkgroupRef];
  if (!lfm) return next;

  lfm.minutes = undefined;
  lfm.active = typeof options.status === 'boolean' ? options.status : !lfm.active;
  if (typeof options.minutes === 'number') lfm.minutes = options.minutes;

  return next;
}

export function toggleSystem(map, systemRef) {
  const next = cloneMap(map);
  for (const [tg, lfm] of Object.entries(next?.[systemRef] ?? {})) {
    lfm.minutes = undefined;
    lfm.active = !lfm.active;
  }
  return next;
}

export function setSystemStatus(map, systemRef, status) {
  const next = cloneMap(map);
  for (const lfm of Object.values(next?.[systemRef] ?? {})) {
    lfm.minutes = undefined;
    lfm.active = status;
  }
  return next;
}

export function toggleAll(map, status) {
  const next = cloneMap(map);
  for (const tgMap of Object.values(next)) {
    for (const lfm of Object.values(tgMap)) {
      lfm.minutes = undefined;
      lfm.active = status;
    }
  }
  return next;
}

export function isAvoided(map, systemRef, talkgroupRef) {
  return map?.[systemRef]?.[talkgroupRef]?.active !== true;
}

export function avoidMinutes(map, systemRef, talkgroupRef) {
  return map?.[systemRef]?.[talkgroupRef]?.minutes ?? 0;
}

export function isPatched(map, call) {
  return isAvoided(map, call.system, call.talkgroup) &&
    (call.patches ?? []).some((tg) => map?.[call.system]?.[tg]?.active === true);
}

// Reconciles the stored map with the CFG config: entries that still exist are
// kept, new talkgroups default to active unless a category containing them is
// off, entries gone from the config are dropped.
export function rebuildMap(systems, storedMap, categories) {
  const map = {};
  for (const system of systems ?? []) {
    map[system.id] = {};
    for (const talkgroup of system.talkgroups ?? []) {
      const stored = storedMap?.[system.id]?.[talkgroup.id];
      if (stored) {
        map[system.id][talkgroup.id] = cloneEntry(stored);
        continue;
      }

      const group = categories.find((cat) => (talkgroup.groups ?? []).includes(cat.label));
      const tag = categories.find((cat) => cat.label === talkgroup.tag);
      const active = !(group?.status === 'off' || tag?.status === 'off');
      map[system.id][talkgroup.id] = { active };
    }
  }
  return map;
}
