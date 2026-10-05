// Category tri-state derivation and toggle semantics.
// Categories derive from the config groups map { label: { systemRef: [talkgroupRef...] } }
// (legacy rebuildCategories derives from config.groups only; tag categories exist
// as a toggle type matching talkgroup tag labels).

export function deriveCategories(groupsMap, livefeedMap) {
  const categories = Object.keys(groupsMap ?? {}).map((label) => {
    const systems = groupsMap[label];

    const allOff = Object.keys(systems).every((sys) =>
      systems[sys].every((tg) => livefeedMap?.[sys]?.[tg]?.active !== true));

    const allOn = Object.keys(systems).every((sys) =>
      systems[sys].every((tg) => livefeedMap?.[sys]?.[tg]?.active === true));

    const status = allOff ? 'off' : allOn ? 'on' : 'partial';

    return { label, status, type: 'group' };
  });

  categories.sort((a, b) => a.label.localeCompare(b.label));
  return categories;
}

// Pressing an on category deactivates all its talkgroups; pressing an off or
// partial category activates all its talkgroups. Affected entries get their
// timed-avoid minutes cleared to 0 (legacy clearTimer in toggleCategory).
export function toggleCategory(map, systems, category) {
  const next = {};
  for (const [sys, tgMap] of Object.entries(map)) {
    next[sys] = {};
    for (const [tg, lfm] of Object.entries(tgMap)) {
      next[sys][tg] = { active: lfm.active, ...(lfm.minutes !== undefined ? { minutes: lfm.minutes } : {}) };
    }
  }

  const active = category.status !== 'on';

  for (const system of systems ?? []) {
    for (const talkgroup of system.talkgroups ?? []) {
      const lfm = next?.[system.id]?.[talkgroup.id];
      if (!lfm) continue;

      const affected =
        (category.type === 'group' && (talkgroup.groups ?? []).includes(category.label)) ||
        (category.type === 'tag' && talkgroup.tag === category.label);

      if (affected) {
        lfm.minutes = 0;
        lfm.active = active;
      }
    }
  }

  return next;
}
