// Admin tools CSV import (legacy import-talkgroups / import-units components).
// Legacy parsing: split lines, strip surrounding quotes, split on /"*,"*/,
// keep rows whose first column is numeric, dedupe by first column.

export function parseCsv(text) {
  return text
    .split(/\n|\r\n/)
    .map((row) => row.replace(/^"|"$/g, '').split(/"*,"*/))
    .filter((row) => row && /^[0-9]+$/.test(row[0]))
    .filter((row, idx, arr) => arr.findIndex((a) => a[0] === row[0]) === idx);
}

function nextId(collection) {
  return (collection ?? []).reduce((prev, cur) => (typeof cur.id === 'number' && cur.id >= prev ? cur.id + 1 : prev), 1);
}

// Columns: 0 talkgroupRef, 2 label, 4 name, 5 tag, 6 group.
// Missing groups/tags are created with the next free id; imported talkgroups
// are prepended (legacy unshift: last row ends up first).
export function importTalkgroups(config, system, rows) {
  const next = structuredClone(config);
  const target = next.systems.find((s) => s.id === system.id);

  next.groups = next.groups ?? [];
  next.tags = next.tags ?? [];

  for (const row of rows) {
    const group = row[6];
    if (group && !next.groups.find((g) => g.label === group)) {
      next.groups.push({ id: nextId(next.groups), label: group });
    }

    const tag = row[5];
    if (tag && !next.tags.find((t) => t.label === tag)) {
      next.tags.push({ id: nextId(next.tags), label: tag });
    }
  }

  for (const row of rows) {
    const groupId = next.groups.find((g) => g.label === row[6])?.id;
    const tagId = next.tags.find((t) => t.label === row[5])?.id;

    target.talkgroups = target.talkgroups ?? [];
    target.talkgroups.unshift({
      talkgroupRef: +row[0],
      label: row[2],
      name: row[4],
      groupIds: groupId ? [groupId] : [],
      tagId,
    });
  }

  return next;
}

// Columns: 0 unitRef, 1 label. Units are appended with 1-based order.
export function importUnits(config, system, rows) {
  const next = structuredClone(config);
  const target = next.systems.find((s) => s.id === system.id);

  const units = rows.map((row, idx) => ({
    unitRef: +row[0],
    label: row[1],
    order: idx + 1,
  }));

  target.units = (target.units ?? []).concat(units);

  return next;
}
