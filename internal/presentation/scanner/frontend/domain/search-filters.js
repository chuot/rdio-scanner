// Cascading search filter recomputation: selection is
// { system?, talkgroup?, group?, tag? } in filter labels.

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

// search criteria: systems and talkgroups travel as references,
// groups and tags as labels the server expands.
// see the search-criteria-label-vs-reference maintainer note.
export function resolveCallSearchCriteria(config, selection) {
  const criteria = {};

  const selectedSystem = config?.systems?.find((system) => system.label === selection.system);

  // a reference of zero is the server's unfiltered marker, so a system
  // the configuration never numbered cannot narrow the history.
  if (selectedSystem?.id) criteria.system = selectedSystem.id;

  const selectedTalkgroup = selectedSystem?.talkgroups?.find((talkgroup) => talkgroup.label === selection.talkgroup);

  if (selectedSystem?.id && selectedTalkgroup?.id) criteria.talkgroup = selectedTalkgroup.id;

  if (selection.group) criteria.group = selection.group;
  if (selection.tag) criteria.tag = selection.tag;

  return criteria;
}
