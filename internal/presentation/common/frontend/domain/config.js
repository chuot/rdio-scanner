// Listener config normalization and admin config round-trip.
// Mirrors the CFG handler and transformCall of the legacy service, and
// the admin config read/save semantics. The retired options are not part
// of the listener state any more.

export function normalizeListenerConfig(raw) {
  const config = raw ?? {};
  return {
    alerts: config.alerts,
    branding: typeof config.branding === 'string' ? config.branding : '',
    email: typeof config.email === 'string' ? config.email : '',
    groups: typeof config.groups !== null && typeof config.groups === 'object' ? config.groups : {},
    groupsData: Array.isArray(config.groupsData) ? config.groupsData : [],
    keypadBeeps: typeof config.keypadBeeps !== 'undefined' && config.keypadBeeps !== null && typeof config.keypadBeeps === 'object' ? config.keypadBeeps : {},
    showListenersCount: typeof config.showListenersCount === 'boolean' ? config.showListenersCount : false,
    systems: Array.isArray(config.systems) ? config.systems.slice() : [],
    tags: typeof config.tags !== null && typeof config.tags === 'object' ? config.tags : {},
    tagsData: Array.isArray(config.tagsData) ? config.tagsData : [],
  };
}

// Resolves call references against the CFG config (legacy transformCall).
export function transformCall(config, call) {
  if (call && Array.isArray(config?.systems)) {
    call.systemData = config.systems.find((system) => system.id === call.system);

    if (Array.isArray(call.systemData?.talkgroups)) {
      call.talkgroupData = call.systemData.talkgroups.find((talkgroup) => talkgroup.id === call.talkgroup);
    }

    if (call.talkgroupData?.frequency) {
      call.frequency = call.talkgroupData.frequency;
    }

    call.groupsData = config.groupsData.filter((gd) => call.talkgroupData?.groups?.some((l) => l === gd.label));

    call.tagData = config.tagsData.find((td) => td.label === call.talkgroupData?.tag);
  }

  return call;
}

// Admin round-trip. The server writes the talkgroup order under key
// talkgroup and reads key order; the working copy normalizes on read.

export function createAdminWorkingCopy(config) {
  const working = structuredClone(config);

  for (const system of working.systems ?? []) {
    for (const talkgroup of system.talkgroups ?? []) {
      if (talkgroup.talkgroup !== undefined) {
        talkgroup.order = talkgroup.order ?? talkgroup.talkgroup;
        delete talkgroup.talkgroup;
      }
    }
  }

  return working;
}

// Merges a possibly partial working copy back onto the baseline so keys
// the admin form never touched are re-emitted on save.
export function mergeAdminConfig(baseline, working) {
  return deepMerge(structuredClone(baseline), structuredClone(working));
}

export function serializeAdminConfig(config) {
  const saved = structuredClone(config);

  for (const system of saved.systems ?? []) {
    for (const talkgroup of system.talkgroups ?? []) {
      if (talkgroup.talkgroup !== undefined) {
        talkgroup.order = talkgroup.order ?? talkgroup.talkgroup;
        delete talkgroup.talkgroup;
      }
    }
  }

  return saved;
}

// Admin config sections exposed as individual REST endpoints: each section
// is read and saved on its own, not as the legacy whole-form swap.
export const ADMIN_CONFIG_SECTIONS = [
  'access',
  'apikeys',
  'dirwatch',
  'downstreams',
  'groups',
  'options',
  'systems',
  'tags',
];

// Per-section working copy: the systems section carries the talkgroup
// order-key normalization.
export function createSectionWorkingCopy(section, baseline) {
  if (section === 'systems') return createAdminWorkingCopy({ systems: baseline }).systems;

  return structuredClone(baseline ?? null);
}

// The three levels of one system the administrator saves on its own:
// sites, talkgroups, units.
export const SYSTEM_SUB_LEVELS = ['sites', 'talkgroups', 'units'];

// Per-level working copy: the talkgroups level carries the order-key
// normalization, the other levels are plain clones.
export function createSubSectionWorkingCopy(level, baseline) {
  if (level === 'talkgroups') {
    if (baseline === null || baseline === undefined) return null;

    return createAdminWorkingCopy({ systems: [{ talkgroups: baseline }] }).systems[0].talkgroups;
  }

  return structuredClone(baseline ?? null);
}

// Per-level save payload: the serialization-side normalization, same rule.
export function serializeSubSection(level, working) {
  if (level === 'talkgroups') {
    if (working === null || working === undefined) return null;

    return serializeAdminConfig({ systems: [{ talkgroups: working }] }).systems[0].talkgroups;
  }

  return structuredClone(working);
}

// Per-section save payload: the fields of the system row, with the
// serialization-side normalization (systems section only), and no sites,
// talkgroups, or units.
export function serializeSection(section, working) {
  if (section !== 'systems') return structuredClone(working);

  return serializeAdminConfig({ systems: working }).systems.map((system) => {
    for (const level of SYSTEM_SUB_LEVELS) delete system[level];

    return system;
  });
}

function deepMerge(target, source) {
  for (const [key, value] of Object.entries(source)) {
    if (
      value !== null && typeof value === 'object' && !Array.isArray(value) &&
      target[key] !== null && typeof target[key] === 'object' && !Array.isArray(target[key])
    ) {
      deepMerge(target[key], value);
    } else {
      target[key] = value;
    }
  }
  return target;
}
