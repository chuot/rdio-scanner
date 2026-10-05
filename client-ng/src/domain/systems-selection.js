// Tri-state systems/talkgroups selection model (T036).
// Pure state machine behind rdio-systems-selection: the tree is
// Everything → Groups / Tags / Systems → Talkgroups, and checks cascade in
// all four directions (legacy config/systems-selection semantics).
//
// Ids are business refs: system.systemRef, talkgroup.talkgroupRef, group.id,
// tag.id. Indeterminate states are display-only, never stored as values;
// parent checks are derived from their children (legacy {emitEvent:false}
// writes).

export function createSelectionModel(config) {
  const groups = (config.groups ?? []).map((group) => ({ id: group.id, checked: false }));

  const tags = (config.tags ?? []).map((tag) => ({ id: tag.id, checked: false }));

  const systems = (config.systems ?? []).map((system) => ({
    id: system.systemRef,
    all: false,
    talkgroups: (system.talkgroups ?? []).map((talkgroup) => ({
      id: talkgroup.talkgroupRef,
      checked: false,
      groupIds: Array.isArray(talkgroup.groupIds)
        ? talkgroup.groupIds
        : typeof talkgroup.groupIds === 'number'
          ? [talkgroup.groupIds]
          : [],
      tagId: talkgroup.tagId ?? 0,
    })),
  }));

  return {
    all: false,
    groups,
    tags,
    systems,
    indeterminate: {
      everything: false,
      groups: groups.map(() => false),
      tags: tags.map(() => false),
      systems: systems.map(() => false),
    },
  };
}

export function setEverything(model, checked) {
  model.all = checked;

  for (const group of model.groups) group.checked = checked;
  for (const tag of model.tags) tag.checked = checked;

  eachTalkgroup(model, (talkgroup) => {
    talkgroup.checked = checked;
  });

  recompute(model);
}

export function setGroup(model, index, checked) {
  const group = model.groups[index];

  group.checked = checked;

  eachTalkgroup(model, (talkgroup) => {
    if (talkgroup.groupIds.includes(group.id)) talkgroup.checked = checked;
  });

  recompute(model);
}

export function setTag(model, index, checked) {
  const tag = model.tags[index];

  tag.checked = checked;

  eachTalkgroup(model, (talkgroup) => {
    if (talkgroup.tagId === tag.id) talkgroup.checked = checked;
  });

  recompute(model);
}

export function setSystem(model, index, checked) {
  const system = model.systems[index];

  system.all = checked;

  for (const talkgroup of system.talkgroups) talkgroup.checked = checked;

  recompute(model);
}

export function setTalkgroup(model, systemIndex, talkgroupIndex, checked) {
  model.systems[systemIndex].talkgroups[talkgroupIndex].checked = checked;

  recompute(model);
}

export function hydrateSelection(model, value) {
  if (value === '*') {
    setEverything(model, true);

    return;
  }

  if (!Array.isArray(value)) return;

  for (const entry of value) {
    const id = typeof entry === 'number' ? entry : entry?.id;

    const systemIndex = model.systems.findIndex((system) => system.id === id);

    if (systemIndex === -1) continue;

    const talkgroups = typeof entry === 'number' ? '*' : entry.talkgroups;

    if (talkgroups === '*') {
      setSystem(model, systemIndex, true);

      continue;
    }

    if (Array.isArray(talkgroups)) {
      for (const ref of talkgroups) {
        const talkgroup = model.systems[systemIndex].talkgroups.find((t) => t.id === ref);

        if (talkgroup) talkgroup.checked = true;
      }

      recompute(model);
    }
  }
}

export function emitSelection(model) {
  if (model.all === true) return '*';

  const selected = [];

  for (const system of model.systems) {
    if (system.all) {
      selected.push({ id: system.id, talkgroups: '*' });

      continue;
    }

    const checked = system.talkgroups.filter((t) => t.checked).map((t) => t.id);

    if (checked.length) selected.push({ id: system.id, talkgroups: checked });
  }

  return selected;
}

function recompute(model) {
  // talkgroups → systems
  for (const [index, system] of model.systems.entries()) {
    const on = system.talkgroups.filter((t) => t.checked).length;
    const off = system.talkgroups.length - on;

    model.indeterminate.systems[index] = !!off && !!on;

    if (system.talkgroups.length) system.all = !off && !!on;
  }

  // systems → everything
  const on = model.systems.filter((s) => s.all).length;
  const off = model.systems.length - on;

  model.indeterminate.everything = !!off && !!on;

  if (model.systems.length) model.all = !off && !!on;

  // talkgroups → groups and tags (recounted across all systems, legacy
  // rebuildGroupIndeterminates / rebuildTagIndeterminates)
  for (const [index, group] of model.groups.entries()) {
    let covered = 0;
    let uncovered = 0;

    eachTalkgroup(model, (talkgroup) => {
      if (!talkgroup.groupIds.includes(group.id)) return;

      if (talkgroup.checked) covered += 1;
      else uncovered += 1;
    });

    model.indeterminate.groups[index] = !!uncovered && !!covered;

    if (covered || uncovered) group.checked = !uncovered && !!covered;
  }

  for (const [index, tag] of model.tags.entries()) {
    let covered = 0;
    let uncovered = 0;

    eachTalkgroup(model, (talkgroup) => {
      if (talkgroup.tagId !== tag.id) return;

      if (talkgroup.checked) covered += 1;
      else uncovered += 1;
    });

    model.indeterminate.tags[index] = !!uncovered && !!covered;

    if (covered || uncovered) tag.checked = !uncovered && !!covered;
  }
}

function eachTalkgroup(model, callback) {
  for (const system of model.systems) {
    for (const talkgroup of system.talkgroups) callback(talkgroup, system);
  }
}
