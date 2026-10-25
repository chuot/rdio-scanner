// Shared scaffold for the per-level sections: system dropdown, rows, save bar; the row
// editor is the only part each section supplies.
// The scaffold owns the store binding: only it knows which system is shown.
// see the subsection-binding-ownership maintainer note.

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
import { attachSingleOpenRule, markRowIdentity, rowKey, setRowMarker } from './row-builders.js';

export function newRow(level) {
  const base = { id: 0, order: 0 };

  if (level === 'sites') return markRowIdentity({ ...base, label: '', siteRef: null });

  if (level === 'talkgroups') {
    return markRowIdentity({ ...base, alert: '', delay: null, frequency: null, groupIds: [], label: '', led: '', name: '', tagId: null, talkgroupRef: null, type: '' });
  }

  return markRowIdentity({ ...base, label: '', unitFrom: null, unitRef: null, unitTo: null });
}

// The panel key the scaffold's open state and the section's row builders share:
// rowPrefix keeps levels apart in one section and names the add control's new row.
export function rowPanelKey(component, row, index) {
  return `${component.subSection.rowPrefix}:${rowKey(row, index)}`;
}

function sliceKey(component) {
  return `subsection:${component.subSection.level}:${component.systemId}`;
}

// The talkgroups level edits the system's blacklist beside its rows, so the
// key tracks the shown system.
function blacklistsKey(component) {
  return component.subSection.level === 'talkgroups' ? `blacklists:${component.systemId}` : null;
}

function bind(component) {
  for (const unsubscribe of component.unsubscribes ?? []) unsubscribe();

  component.boundKey = sliceKey(component);

  // boundKey is the prefix every field key of this render is built on, so reached state
  // stays per system.
  component.rowKeyPrefix = component.boundKey;

  const { store } = getAdminContext();

  const keys = ['section:systems', ...component.subSection.listen, component.boundKey];

  const blacklists = blacklistsKey(component);

  if (blacklists) keys.push(blacklists);

  component.unsubscribes = [store.subscribe(keys, () => component.render())];
}

// The section's description: its level, caption, add control label, row prefix,
// the extra slices its rows render from, and the row editor.
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

// Model commit for a per-level section.
// A field edit carries the selfEdit marker and its render skips the rebuild, so the
// row being typed in stays on the page.
// see the subsection-selfedit-rebuild maintainer note.
export function commitSubRows(component, rows, selfEdit = false) {
  const { level } = component.subSection;

  if (selfEdit) component.internalEdit = rows;

  setSubWorking(level, component.systemId, rows);
  setSubInvalid(level, component.systemId, subSectionInvalid(level, rows, sectionWorking('groups') ?? [], sectionWorking('tags') ?? []));
}

// refreshField recomputes the edited field's error message from the rows as they now
// are and marks it reached; setRowMarker moves the row's warning icon with it.
export function refreshField(component, field, message, panel, invalid) {
  component.reached.add(field.dataset.field);

  field.setError(message);
  field.markReached();

  if (panel) setRowMarker(panel, invalid);
}

// harvestReached collects reached state by the data-field key before a rebuild,
// the way the open state is; restoreReached hands it back after.
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

// A section matching the server's baseline starts with every field unreached.
// Only the keys under this prefix are cleared, so rows in another system keep their
// reached fields.
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

  // The shown system moves under the section: dropdown choice, or fallback after a delete.
  // The binding follows the rows to the new slice.
  if (component.boundKey !== sliceKey(component)) bind(component);

  // The scaffold resolves the shown system and carries its systemRef, the number the
  // operator types and searches the log by, to the bar and the section's messages.
  component.systemRef = systems.find((system) => system.id === component.systemId)?.systemRef ?? null;

  // A store change here is the operator's own field edit: see commitSubRows, whose render
  // keeps their rows in place.
  const slice = subSlice(level, component.systemId);

  if (slice && slice.working === component.internalEdit) return;

  // A section whose working copy matches the server's baseline starts with every field
  // unreached: the operator has touched nothing.
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

    // The row the add control creates is the section's one open row: the rows the operator
    // left open close with it.
    // see the add-close-open-rows maintainer note.
    const opened = rowPanelKey(component, fresh[0], 0);

    for (const row of component.querySelectorAll('details[data-key]')) row.open = false;

    component.openState.clear();
    component.openState.set(opened, true);

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

  // The drop commits subWorking as the store holds it at the moment of the move, the way
  // the Systems section does, so a later field edit is not dropped.
  attachDragSort(list, (from, to) => commitSubRows(component, reorderAfterDrag(subWorking(level, component.systemId) ?? [], from, to)));

  // One row open at a time keeps the list short and the save bar below it reachable
  // without scrolling past opened rows.
  attachSingleOpenRule(list, component.openState);

  root.append(list);

  const bar = document.createElement('rdio-admin-section-bar');
  bar.dataset.subsection = `${level}:${component.systemId}`;

  if (component.systemRef) bar.dataset.systemRef = String(component.systemRef);

  root.append(bar);

  component.replaceChildren(root);

  for (const { field, error } of component.errorTargets) field.setError(error);

  restoreReached(component);

  setSubInvalid(level, component.systemId, subSectionInvalid(level, rows, sectionWorking('groups') ?? [], sectionWorking('tags') ?? []));
}
