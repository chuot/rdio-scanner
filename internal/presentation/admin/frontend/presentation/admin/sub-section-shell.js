// Shared scaffold for the three per-level sections (FEAT-016): a system
// dropdown over the saved systems in display order, the rows of the selected
// system's level, add/delete/drag-sort with the 1-based order rewrite, and the
// save bar bound to the `subsection:<level>:<systemId>` slice. The row editor
// is the only part each section supplies.

import {
  attachDragSort,
  reorderAfterDrag,
  sectionWorking,
  setSubInvalid,
  setSubWorking,
  sortedByOrder,
  subWorking,
} from './section-shell.js';
import { subSectionInvalid } from '../../domain/systems-validation.js';

export function newRow(level) {
  const base = { id: 0, order: 0 };

  if (level === 'sites') return { ...base, label: '', siteRef: null };

  if (level === 'talkgroups') {
    return { ...base, alert: '', delay: null, frequency: null, groupIds: [], label: '', led: '', name: '', tagId: null, talkgroupRef: null, type: '' };
  }

  return { ...base, label: '', unitFrom: null, unitRef: null, unitTo: null };
}

export function renderSubSection(component, { level, caption, addLabel, buildRow }) {
  const systems = sortedByOrder(sectionWorking('systems') ?? []).filter((system) => typeof system.id === 'number' && system.id > 0);

  if (!systems.some((system) => system.id === component.systemId)) component.systemId = systems[0]?.id ?? null;

  component.errorTargets = [];

  const root = document.createElement('div');
  root.className = `rdio-admin-card ${level}`;

  const top = document.createElement('div');
  top.className = 'row top';

  const help = document.createElement('p');
  help.textContent = caption;

  const select = document.createElement('rdio-select');
  select.setAttribute('options', JSON.stringify(systems.map((system) => [String(system.id), system.label])));
  select.setAttribute('value', String(component.systemId ?? ''));
  select.addEventListener('change', (ev) => {
    if (typeof ev.detail !== 'string') return;

    component.systemId = Number(ev.detail);

    component.rebind();
    component.render();
  });

  const add = document.createElement('button');
  add.type = 'button';
  add.className = 'rdio-button-mini add';
  add.textContent = addLabel;
  add.addEventListener('click', () => {
    const rows = subWorking(level, component.systemId) ?? [];

    setSubWorking(level, component.systemId, [newRow(level), ...rows.map((row, i) => ({ ...row, order: i + 2 }))]);
  });

  top.append(help, select, add);
  root.append(top);

  if (component.systemId === null) {
    const empty = document.createElement('p');
    empty.className = 'rdio-admin-empty';
    empty.textContent = 'No system is defined. Define a system in the Systems section first.';

    root.append(empty);
    component.replaceChildren(root);

    return;
  }

  const rows = sortedByOrder(subWorking(level, component.systemId) ?? []);

  const list = document.createElement('div');
  list.className = 'rdio-admin-rows';

  rows.forEach((row, index) => list.append(buildRow(component, row, index, rows)));

  attachDragSort(list, (from, to) => setSubWorking(level, component.systemId, reorderAfterDrag(rows, from, to)));

  root.append(list);

  const bar = document.createElement('rdio-admin-section-bar');
  bar.dataset.subsection = `${level}:${component.systemId}`;
  root.append(bar);

  component.replaceChildren(root);

  for (const { field, error } of component.errorTargets) field.setError(error);

  setSubInvalid(level, component.systemId, subSectionInvalid(level, rows, sectionWorking('groups') ?? [], sectionWorking('tags') ?? []));
}
