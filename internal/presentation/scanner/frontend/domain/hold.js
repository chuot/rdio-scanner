// HOLD SYS / HOLD TG snapshot semantics: the caller keeps the pre-hold
// map as the restore snapshot; these functions only derive the held
// map.

export function holdSystemMap(map, call) {
  const held = {};

  for (const [sys, tgMap] of Object.entries(map)) {
    // intended legacy semantics: a fully inactive current system is
    // switched on as a whole. the legacy allOn test read object
    // truthiness and was always false.
    const allOff = Object.values(tgMap).every((lfm) => lfm.active !== true);

    held[sys] = {};
    for (const [tg, lfm] of Object.entries(tgMap)) {
      held[sys][tg] = {
        active: String(sys) === String(call.system) ? allOff || lfm.active === true : false,
      };
    }
  }

  return held;
}

export function holdTalkgroupMap(map, call) {
  const held = {};

  for (const [sys, tgMap] of Object.entries(map)) {
    held[sys] = {};
    for (const [tg] of Object.entries(tgMap)) {
      held[sys][tg] = {
        active: String(sys) === String(call.system) && String(tg) === String(call.talkgroup),
      };
    }
  }

  return held;
}
