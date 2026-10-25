// systems tree validation: pure rules, system to sites, talkgroups, and units;
// cross-refs check the current groups/tags working copies, arrays included
// see the systems-foreign-key-validation maintainer note.

const BLACKLISTS_PATTERN = /^[0-9]+(,[0-9]+)*$/;

// the one blacklist rule, shared wherever a list is edited: valid means accepted.
export function blacklistsTextInvalid(text) {
  return typeof text === 'string' && text.length > 0 && !BLACKLISTS_PATTERN.test(text);
}

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

  if (blacklistsTextInvalid(system.blacklists)) {
    errors.blacklists = 'Comma separated list of talkgroup references';
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

// one level of one system's rows, validated as the tree validated them, against
// the current groups/tags working copies for the talkgroup foreign keys.
export function subSectionInvalid(level, rows, groups, tags) {
  const knownGroupIds = (groups ?? []).map((group) => group.id);
  const knownTagIds = (tags ?? []).map((tag) => tag.id);

  for (const row of rows ?? []) {
    const errors =
      level === 'sites'
        ? siteErrors(row, rows)
        : level === 'talkgroups'
          ? talkgroupErrors(row, rows, { groupIds: knownGroupIds, tagIds: knownTagIds })
          : unitErrors(row, rows);

    if (Object.keys(errors).length) return true;
  }

  return false;
}
