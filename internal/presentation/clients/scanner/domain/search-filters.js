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
