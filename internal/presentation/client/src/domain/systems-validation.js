// Systems section validation (T042). Pure port of the legacy admin.service.ts
// validators for the systems tree: system → sites / talkgroups / units.
//
// Cross-references (talkgroup groupIds / tagId) are validated against the
// current groups/tags working copies so a save can never hit a server foreign
// key. Legacy quirk kept: a single-number groupIds value is membership-checked
// the same way; arrays are checked too (legacy skipped arrays, which let stale
// ids reach the server and fail with 417 — see decision-legacy-bug-fixes.md).

const BLACKLISTS_PATTERN = /^[0-9]+(,[0-9]+)*$/;

export function systemErrors(system, systems) {
  const errors = {};

  if (system.systemRef === null || system.systemRef === undefined) {
    errors.systemRef = 'Id is required';
  } else if (typeof system.systemRef === 'number' && system.systemRef < 1) {
    errors.systemRef = 'Id is invalid';
  } else if (
    typeof system.systemRef === 'number' &&
    system.systemRef > 0 &&
    systems.filter((s) => s.systemRef === system.systemRef).length > 1
  ) {
    errors.systemRef = 'Id is already defined';
  }

  if (!system.label) errors.label = 'Label is required';

  if (typeof system.blacklists === 'string' && system.blacklists.length && !BLACKLISTS_PATTERN.test(system.blacklists)) {
    errors.blacklists = 'Comma separated list of talkgroup Ids';
  }

  return errors;
}

export function siteErrors(site, sites) {
  const errors = {};

  if (site.siteRef === null || site.siteRef === undefined) {
    errors.siteRef = 'Id is required';
  } else if (typeof site.siteRef === 'number' && site.siteRef < 1) {
    errors.siteRef = 'Id is invalid';
  } else if (
    typeof site.siteRef === 'number' &&
    sites.filter((s) => s.siteRef === site.siteRef).length > 1
  ) {
    errors.siteRef = 'Id is already defined';
  }

  if (!site.label) errors.label = 'Label is required';

  return errors;
}

export function talkgroupErrors(talkgroup, talkgroups, { groupIds: knownGroupIds = [], tagIds: knownTagIds = [] } = {}) {
  const errors = {};

  if (talkgroup.talkgroupRef === null || talkgroup.talkgroupRef === undefined) {
    errors.talkgroupRef = 'Id is required';
  } else if (typeof talkgroup.talkgroupRef === 'number' && talkgroup.talkgroupRef < 1) {
    errors.talkgroupRef = 'Id is invalid';
  } else if (
    typeof talkgroup.talkgroupRef === 'number' &&
    talkgroups.filter((t) => t.talkgroupRef === talkgroup.talkgroupRef).length > 1
  ) {
    errors.talkgroupRef = 'Id is already defined';
  }

  if (!talkgroup.label) errors.label = 'Label is required';

  if (!talkgroup.name) errors.name = 'Name is required';

  if (talkgroup.groupIds === null || talkgroup.groupIds === undefined || (Array.isArray(talkgroup.groupIds) && talkgroup.groupIds.length === 0)) {
    errors.groupIds = 'Group is required';
  } else if (typeof talkgroup.groupIds === 'number' && !knownGroupIds.includes(talkgroup.groupIds)) {
    errors.groupIds = 'Group is required';
  } else if (
    Array.isArray(talkgroup.groupIds) &&
    talkgroup.groupIds.some((id) => !knownGroupIds.includes(id))
  ) {
    errors.groupIds = 'Group is required';
  }

  if (talkgroup.tagId === null || talkgroup.tagId === undefined || !knownTagIds.includes(talkgroup.tagId)) {
    errors.tagId = 'Tag is required';
  }

  if (typeof talkgroup.frequency === 'number' && talkgroup.frequency < 0) {
    errors.frequency = 'Invalid frequency';
  }

  return errors;
}

export function unitErrors(unit, units) {
  const errors = {};

  const hasRange = unit.unitFrom !== null && unit.unitFrom !== undefined && unit.unitTo !== null && unit.unitTo !== undefined;

  if (unit.unitRef === null || unit.unitRef === undefined) {
    if (!hasRange) errors.unitRef = 'Id value is required';
  } else if (typeof unit.unitRef === 'number' && unit.unitRef < 1) {
    errors.unitRef = 'Id value is invalid';
  } else if (units.filter((u) => u.unitRef === unit.unitRef).length > 1) {
    errors.unitRef = 'Id value is already defined';
  }

  if (typeof unit.unitFrom === 'number' && unit.unitFrom < 1) {
    errors.unitFrom = 'From value is invalid';
  }

  if (typeof unit.unitTo === 'number' && unit.unitTo < 1) {
    errors.unitTo = 'To value is invalid';
  }

  if (
    typeof unit.unitFrom === 'number' &&
    typeof unit.unitTo === 'number' &&
    unit.unitFrom >= unit.unitTo
  ) {
    errors.unitFrom = 'From value should be lower than To value';
    errors.unitTo = 'To value should be higher than From value';
  }

  if (!unit.label) errors.label = 'Label is required';

  return errors;
}

export function systemsSectionInvalid(systems, groups, tags) {
  const knownGroupIds = (groups ?? []).map((group) => group.id);
  const knownTagIds = (tags ?? []).map((tag) => tag.id);

  for (const system of systems ?? []) {
    if (Object.keys(systemErrors(system, systems)).length) return true;

    for (const site of system.sites ?? []) {
      if (Object.keys(siteErrors(site, system.sites)).length) return true;
    }

    for (const talkgroup of system.talkgroups ?? []) {
      if (Object.keys(talkgroupErrors(talkgroup, system.talkgroups, { groupIds: knownGroupIds, tagIds: knownTagIds })).length) return true;
    }

    for (const unit of system.units ?? []) {
      if (Object.keys(unitErrors(unit, system.units)).length) return true;
    }
  }

  return false;
}
