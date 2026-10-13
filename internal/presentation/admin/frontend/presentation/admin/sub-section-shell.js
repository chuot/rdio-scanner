// Shared scaffold for the three per-level sections: a system
// dropdown over the saved systems in display order, the rows of the selected
// system's level, add/delete/drag-sort with the 1-based order rewrite, and the
// save bar bound to the `subsection:<level>:<systemId>` slice. The row editor
// is the only part each section supplies.
//
// The rows a section renders belong to the system it is showing, and only this
// scaffold knows that id: the dropdown picks it, and a section falls back to
// another system when the one it shows is deleted. So the scaffold owns the
// store binding — it listens to the slice the section renders and moves the
// binding when the shown system changes. A section that bound its own slice
// before the scaffold picked the system would listen to a slice it never shows,
// and keep its rows on screen while the data under them moved.

import { getAdminContext } from '../../application/admin-context.js';
import { subSectionInvalid } from '../../domain/systems-validation.js';
import {
  attachDragSort,
  reorderAfterDrag,
  sectionWorking,
  setSubInvalid,
  setSubWorking,
  sortedByOrder,
  subSlice,
  subWorking,
} from './section-shell.js';
import { rowKey, setRowMarker } from './row-builders.js';

export function newRow(level) {
  const base = { id: 0, order: 0 };

  if (level === 'sites') return { ...base, label: '', siteRef: null };

  if (level === 'talkgroups') {
    return { ...base, alert: '', delay: null, frequency: null, groupIds: [], label: '', led: '', name: '', tagId: null, talkgroupRef: null, type: '' };
  }

  return { ...base, label: '', unitFrom: null, unitRef: null, unitTo: null };
}

// The panel key the scaffold's open state and the section's row builders share:
// the level's prefix keeps rows of different levels apart in one section, and
// the scaffold needs it to name the row its add control creates.
export function rowPanelKey(component, row, index) {
  return `${component.subSection.rowPrefix}:${rowKey(row, index)}`;
}

function sliceKey(component) {
  return `subsection:${component.subSection.level}:${component.systemId}`;
}

function bind(component) {
  for (const unsubscribe of component.unsubscribes ?? []) unsubscribe();

  component.boundKey = sliceKey(component);

  // The fields of the slice the section shows are reached or not, and the fields of another
  // system are their own: the slice's key is the prefix every field key of this render is built on.
  component.rowKeyPrefix = component.boundKey;

  const { store } = getAdminContext();

  component.unsubscribes = [store.subscribe(
    ['section:systems', ...component.subSection.listen, component.boundKey],
    () => component.render(),
  )];
}

// `spec` is the section's description: its level, caption, add control label,
// row prefix, the extra slices its rows render from, and the row editor.
export function connectSubSection(component, spec) {
  component.subSection = spec;
  component.openState = new Map();
  component.reached = new Set();

  bind(component);
  component.render();
}

export function disconnectSubSection(component) {
  for (const unsubscribe of component.unsubscribes ?? []) unsubscribe();

  component.unsubscribes = [];
}

// Model commit for a per-level section. A field edit carries the marker: the render
// that sees the value it just committed returns without rebuilding, so the row the
// operator is typing in — and the field they are in — stays on the page. Add,
// delete, drag, and the level's own actions commit without the marker and rebuild.
// The invalid flag is always recomputed from the committed rows.
export function commitSubRows(component, rows, selfEdit = false) {
  const { level } = component.subSection;

  if (selfEdit) component.internalEdit = rows;

  setSubWorking(level, component.systemId, rows);
  setSubInvalid(level, component.systemId, subSectionInvalid(level, rows, sectionWorking('groups') ?? [], sectionWorking('tags') ?? []));
}

// The field the operator just edited: its message is recomputed from the rows as they now are, and
// editing a field is reaching it. The row's warning icon follows the same recomputation, because
// the row stays on the page and the icon has to move with it.
export function refreshField(component, field, message, panel, invalid) {
  component.reached.add(field.dataset.field);

  field.setError(message);
  field.markReached();

  if (panel) setRowMarker(panel, invalid);
}

// The fields the section built carry the key their reached state is remembered by; the state is
// harvested before the rebuild, the way the open state is, and handed back after it.
export function harvestReached(component) {
  for (const field of component.querySelectorAll('rdio-form-field[data-field]')) {
    if (field.isReached?.()) component.reached.add(field.dataset.field);
  }
}

export function restoreReached(component) {
  for (const field of component.querySelectorAll('rdio-form-field[data-field]')) {
    if (component.reached.has(field.dataset.field)) field.markReached();
  }
}

// A section whose working copy matches the server's baseline — saved, discarded, or pushed from the
// server — starts with every field unreached: the operator has touched nothing. Only the fields of
// the slice being rebuilt are cleared; the rows the operator is working on in another system keep
// the state of the fields they edited.
export function clearReached(component, prefix) {
  for (const key of [...component.reached]) {
    if (key.startsWith(`${prefix}:`)) component.reached.delete(key);
  }
}

export function renderSubSection(component) {
  const { level, caption, addLabel, buildRow } = component.subSection;

  for (const details of component.querySelectorAll('details[data-key]')) {
    component.openState.set(details.dataset.key, details.open);
  }

  harvestReached(component);

  const systems = sortedByOrder(sectionWorking('systems') ?? []).filter((system) => typeof system.id === 'number' && system.id > 0);

  if (!systems.some((system) => system.id === component.systemId)) component.systemId = systems[0]?.id ?? null;

  // The shown system moves under the section: the operator picks another one in
  // the dropdown, or the system it shows is deleted and the section falls back.
  // The rows follow it, so the binding follows with them.
  if (component.boundKey !== sliceKey(component)) bind(component);

  // The operator's own field edit is what reaches here as a store change; the rows
  // they are working on stay where they are. See commitSubRows.
  const slice = subSlice(level, component.systemId);

  if (slice && slice.working === component.internalEdit) return;

  // A section whose working copy matches the server's baseline — saved, discarded, or pushed
  // from the server — starts with every field unreached: the operator has touched nothing.
  if (slice && !slice.dirty) clearReached(component, component.boundKey);

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

    component.render();
  });

  const add = document.createElement('button');
  add.type = 'button';
  add.className = 'rdio-button-mini add';
  add.textContent = addLabel;
  add.addEventListener('click', () => {
    const rows = subWorking(level, component.systemId) ?? [];
    const fresh = [newRow(level), ...rows.map((row, i) => ({ ...row, order: i + 2 }))];

    // The row the add control creates is the one the operator came to fill in: the
    // open state records it before the commit, so the rows the scaffold builds next
    // open on the new row's fields and keep the rows the operator left open.
    component.openState.set(rowPanelKey(component, fresh[0], 0), true);

    commitSubRows(component, fresh);
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

  // The drop commits the rows the store holds at the moment of the move, the way the
  // Systems section does: a field edit the operator made after this render is not in
  // the rows above, and a move must not drop what they typed.
  attachDragSort(list, (from, to) => commitSubRows(component, reorderAfterDrag(subWorking(level, component.systemId) ?? [], from, to)));

  root.append(list);

  const bar = document.createElement('rdio-admin-section-bar');
  bar.dataset.subsection = `${level}:${component.systemId}`;
  root.append(bar);

  component.replaceChildren(root);

  for (const { field, error } of component.errorTargets) field.setError(error);

  restoreReached(component);

  setSubInvalid(level, component.systemId, subSectionInvalid(level, rows, sectionWorking('groups') ?? [], sectionWorking('tags') ?? []));
}
