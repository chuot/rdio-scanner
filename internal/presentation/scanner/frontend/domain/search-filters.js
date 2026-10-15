// Cascading search filter recomputation
// (legacy search.component.ts refreshFilters, ported verbatim).
// selection: { system?, talkgroup?, group?, tag? } — filter labels.

export function computeFilterOptions(config, selection) {
  const selectedSystem = config.systems.find((system) => system.label === selection.system);
  const selectedTalkgroup = selectedSystem?.talkgroups.find((talkgroup) => talkgroup.label === selection.talkgroup);
  const selectedGroup = selection.group;
  const selectedTag = selection.tag;

  const systems = config.systems
    .filter((system) => {
      const group = selectedGroup === undefined ||
        system.talkgroups.some((talkgroup) => talkgroup.groups.includes(selectedGroup));
      const tag = selectedTag === undefined ||
        system.talkgroups.some((talkgroup) => talkgroup.tag === selectedTag);
      return group && tag;
    })
    .map((system) => system.label);

  const talkgroups = selectedSystem === undefined
    ? []
    : selectedSystem.talkgroups
        .filter((talkgroup) => {
          const group = selectedGroup === undefined ||
            talkgroup.groups.includes(selectedGroup);
          const tag = selectedTag === undefined ||
            talkgroup.tag === selectedTag;
          return group && tag;
        })
        .map((talkgroup) => talkgroup.label);

  const groups = Object.keys(config.groups)
    .filter((group) => {
      const system = selectedSystem === undefined ||
        selectedSystem.talkgroups.some((talkgroup) => talkgroup.groups.includes(group));
      const talkgroup = selectedTalkgroup === undefined ||
        selectedTalkgroup.groups.includes(group);
      const tag = selectedTag === undefined ||
        (selectedTalkgroup !== undefined && selectedTalkgroup.tag === selectedTag) ||
        config.systems
          .flatMap((system) => system.talkgroups)
          .some((talkgroup) => talkgroup.groups.includes(group) && talkgroup.tag === selectedTag);
      return system && talkgroup && tag;
    })
    .sort((a, b) => a.localeCompare(b));

  const tags = Object.keys(config.tags)
    .filter((tag) => {
      const system = selectedSystem === undefined ||
        selectedSystem.talkgroups.some((talkgroup) => talkgroup.tag === tag);
      const talkgroup = selectedTalkgroup === undefined ||
        selectedTalkgroup.tag === tag;
      const group = selectedGroup === undefined ||
        (selectedTalkgroup !== undefined && selectedTalkgroup.groups.includes(selectedGroup)) ||
        config.systems
          .flatMap((system) => system.talkgroups)
          .some((talkgroup) => talkgroup.tag === tag && talkgroup.groups.includes(selectedGroup));
      return system && talkgroup && group;
    })
    .sort((a, b) => a.localeCompare(b));

  return { systems, talkgroups, groups, tags };
}

// The criteria the call history is searched by, from the filters the operator chose.
//
// The operator reads labels, and the history is stored under references: the search narrows to a
// system by its reference and to a talkgroup by its reference, and a talkgroup reference is only
// ever read together with the system it sits under. A group and a tag are the labels the server
// expands into the talkgroups they name, so they travel as labels.
//
// A label that names nothing in the configuration the listener was given contributes no criterion.
// The server reads a criterion it cannot use as no filter at all, and sending it a label where it
// reads a reference is exactly that - silently, and with the whole history as the answer. Saying
// nothing is the same outcome, said on purpose.
export function resolveCallSearchCriteria(config, selection) {
  const criteria = {};

  const selectedSystem = config?.systems?.find((system) => system.label === selection.system);

  // A reference of zero is the server's own "unfiltered", so a system the configuration never
  // numbered is a system the history cannot be narrowed to.
  if (selectedSystem?.id) criteria.system = selectedSystem.id;

  const selectedTalkgroup = selectedSystem?.talkgroups?.find((talkgroup) => talkgroup.label === selection.talkgroup);

  if (selectedSystem?.id && selectedTalkgroup?.id) criteria.talkgroup = selectedTalkgroup.id;

  if (selection.group) criteria.group = selection.group;
  if (selection.tag) criteria.tag = selection.tag;

  return criteria;
}
