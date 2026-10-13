// rdio-admin-downstreams: downstream connections section (legacy
// config/downstreams). Rows: disabled toggle, url, apikey, systems selection
// dialog. The systems payload may carry `id_as` remote-id mappings (see
// legacy admin.service.ts Downstream type) — no UI for it; the deep-cloned
// working copy preserves those keys untouched.

import { openSystemsSelectionDialog } from './rdio-systems-selection.js';
import {
  attachDragSort,
  bindSectionSlice,
  reorderAfterDrag,
  sectionSlice,
  sectionWorking,
  selectionConfig,
  setSectionInvalid,
  setSectionWorking,
  sortedByOrder,
} from './section-shell.js';
import { markField, rowKey } from './row-builders.js';
import { clearReached, harvestReached, refreshField, restoreReached } from './sub-section-shell.js';

const NAME = 'downstreams';

export function downstreamRowErrors(row, index, rows) {
  const errors = {};

  if (!row.url) errors.url = 'URL is required';
  else if (!/^https?:\/\/.+$/.test(row.url)) errors.url = 'Invalid URL';
  else if (rows.some((other, i) => i !== index && other.url === row.url)) errors.url = 'URL is already defined';

  if (!row.apikey) errors.apikey = 'API key is required';

  if (row.systems === undefined || row.systems === null) errors.systems = 'Systems is required';

  return errors;
}

class RdioAdminDownstreams extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    this.unsubscribe = bindSectionSlice(NAME, () => this.render());

    // The fields of this section's working copy remember the state of having been reached, the way
    // the systems tree sections do: the key they are remembered by names this section's slice.
    this.reached = new Set();
    this.rowKeyPrefix = `section:${NAME}`;

    this.render();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
  }

  get rows() {
    return sortedByOrder(sectionWorking(NAME));
  }

  render() {
    const slice = sectionSlice(NAME);

    if (!slice || slice.working === this.internalEdit) return;

    harvestReached(this);

    // A section whose working copy matches the server's baseline — saved, discarded, or pushed from
    // the server — starts with every field unreached: the operator has touched nothing.
    if (!slice.dirty) clearReached(this, this.rowKeyPrefix);

    const rows = this.rows;

    // rdio-form-field.setError only exists after the field is connected.
    this.errorTargets = [];

    const root = document.createElement('div');
    root.className = 'rdio-admin-card downstreams';

    const top = document.createElement('div');
    top.className = 'row top';

    const intro = document.createElement('p');
    intro.textContent = rows.length
      ? 'The downstream connects this instance to another Rdio Scanner instance.'
      : 'No defined downstreams.';

    const add = document.createElement('button');
    add.type = 'button';
    add.className = 'rdio-button-mini add';
    add.textContent = 'New downstream';
    add.addEventListener('click', () => this.add());

    top.append(intro, add);
    root.append(top);

    const list = document.createElement('div');
    list.className = 'rdio-admin-rows';

    rows.forEach((row, index) => list.append(this.rowElement(row, index, rows)));

    attachDragSort(list, (from, to) => setSectionWorking(NAME, reorderAfterDrag(rows, from, to)));

    root.append(list);

    const bar = document.createElement('rdio-admin-section-bar');
    bar.dataset.section = NAME;
    root.append(bar);

    this.replaceChildren(root);

    for (const { field, error } of this.errorTargets) field.setError(error);

    restoreReached(this);
  }

  rowElement(row, index, rows) {
    const panel = document.createElement('div');
    panel.className = 'rdio-admin-row';
    panel.dataset.drag = '';
    panel.draggable = true;

    const errors = downstreamRowErrors(row, index, rows);

    const header = document.createElement('div');
    header.className = 'row top';
    header.innerHTML = '<i class="material-icons drag-handle" aria-hidden="true">drag_indicator</i>';

    const title = document.createElement('span');
    title.className = 'row-title';
    title.textContent = row.url || 'NewDownstream';
    header.append(title);

    if (Object.keys(errors).length) this.setHeaderInvalid(header, true);

    panel.append(header);

    // The row's warning icon follows the row's validity, recomputed from the rows as the section
    // holds them: the row the operator is editing stays on the page, so the icon moves on the spot.
    const refreshMarker = () => {
      const rows = this.rows;

      this.setHeaderInvalid(header, Object.keys(downstreamRowErrors(rows[index], index, rows)).length > 0);
    };

    const disabledRow = document.createElement('div');
    disabledRow.className = 'row';

    const disabledLabel = document.createElement('span');
    disabledLabel.textContent = 'Disabled';

    const disabledToggle = document.createElement('rdio-toggle');
    disabledToggle.checked = !!row.disabled;
    disabledToggle.addEventListener('change', (ev) => {
      this.updateRow(index, { disabled: ev.detail });
    });

    disabledRow.append(disabledLabel, disabledToggle);

    const urlField = this.field('URL');
    const urlInput = urlField.querySelector('input');
    urlInput.value = row.url ?? '';
    urlInput.required = true;

    if (errors.url) this.errorTargets.push({ field: urlField, error: errors.url });

    markField(this, urlField, rowKey(row, index), 'url');

    urlInput.addEventListener('change', () => {
      this.updateRow(index, { url: urlInput.value });

      // The message the field carries is the one its row has now, read from the rows as the
      // section holds them: a URL another row has is a rule about the whole list.
      const rows = this.rows;

      refreshField(this, urlField, downstreamRowErrors(rows[index], index, rows).url ?? '');

      refreshMarker();
    });

    const apikeyField = this.field('API key');
    const apikeyInput = apikeyField.querySelector('input');
    apikeyInput.value = row.apikey ?? '';
    apikeyInput.required = true;

    if (errors.apikey) this.errorTargets.push({ field: apikeyField, error: errors.apikey });

    markField(this, apikeyField, rowKey(row, index), 'apikey');

    apikeyInput.addEventListener('change', () => {
      this.updateRow(index, { apikey: apikeyInput.value });

      const rows = this.rows;

      refreshField(this, apikeyField, downstreamRowErrors(rows[index], index, rows).apikey ?? '');

      refreshMarker();
    });

    const systemsRow = document.createElement('div');
    systemsRow.className = 'row';

    const systemsSummary = document.createElement('p');
    systemsSummary.textContent = `The downstream receives all or some systems and talkgroups.`;

    const systemsButton = document.createElement('button');
    systemsButton.type = 'button';
    systemsButton.className = 'rdio-button-mini choose-systems';
    systemsButton.textContent = 'Choose systems';
    systemsButton.addEventListener('click', async () => {
      const value = await openSystemsSelectionDialog({ config: selectionConfig(), value: row.systems ?? '*' });

      if (value === null) return;

      setSectionWorking(NAME, this.rows.map((r, i) => (i === index ? { ...r, systems: value } : r)));
    });

    systemsRow.append(systemsSummary, systemsButton);

    if (errors.systems) systemsButton.setAttribute('aria-invalid', 'true');

    const bottom = document.createElement('div');
    bottom.className = 'row bottom';

    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'rdio-button-mini delete warn';
    remove.textContent = 'Delete downstream';
    remove.addEventListener('click', () => {
      setSectionWorking(NAME, this.rows.filter((_, i) => i !== index));
    });

    bottom.append(remove);

    panel.append(disabledRow, urlField, apikeyField, systemsRow, bottom);

    return panel;
  }

  field(label) {
    const field = document.createElement('rdio-form-field');

    const labelEl = document.createElement('label');
    labelEl.textContent = label;

    const input = document.createElement('input');
    input.type = 'text';

    field.append(labelEl, input);

    return field;
  }

  setHeaderInvalid(header, invalid) {
    if (!header) return;

    const existing = header.querySelector('.row-invalid');

    if (invalid && !existing) {
      const icon = document.createElement('i');
      icon.className = 'material-icons row-invalid';
      icon.setAttribute('aria-label', 'Invalid row');
      icon.textContent = 'error';
      header.append(icon);
    }

    if (!invalid && existing) existing.remove();
  }

  updateRow(index, patch) {
    const rows = this.rows.map((row, i) => (i === index ? { ...row, ...patch } : row));

    this.internalEdit = rows;

    setSectionWorking(NAME, rows);
    setSectionInvalid(NAME, rows.some((row, i) => Object.keys(downstreamRowErrors(row, i, rows)).length > 0));
  }

  add() {
    const rows = [
      { id: 0, order: 0, apikey: '', disabled: false, systems: '*', url: '' },
      ...this.rows.map((row, i) => ({ ...row, order: i + 2 })),
    ];

    setSectionWorking(NAME, rows);
  }
}

customElements.define('rdio-admin-downstreams', RdioAdminDownstreams);
