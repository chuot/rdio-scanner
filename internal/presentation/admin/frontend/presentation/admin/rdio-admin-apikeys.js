// rdio-admin-apikeys: API keys section (legacy config/apikeys).
// Rows: disabled toggle, key (generated on add, copy button), ident, systems
// selection dialog. Hidden id/order round-trip; per-section Save in the bar.

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
import { markField, markRowIdentity, rowKey } from './row-builders.js';
import { clearReached, harvestReached, refreshField, restoreReached } from './sub-section-shell.js';
import { showSnackbar } from '../../../../common/frontend/presentation/ui/primitives.js';

const NAME = 'apikeys';

export function apiKeyRowErrors(row, index, rows) {
  const errors = {};

  if (!row.key) errors.key = 'Key is required';
  else if (rows.some((other, i) => i !== index && other.key === row.key)) errors.key = 'Key is already defined';

  if (!row.ident) errors.ident = 'Ident is required';
  else if (rows.some((other, i) => i !== index && other.ident === row.ident)) errors.ident = 'Ident is already defined';

  if (row.systems === undefined || row.systems === null) errors.systems = 'Systems is required';

  return errors;
}

class RdioAdminApikeys extends HTMLElement {
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
    root.className = 'rdio-admin-card apikeys';

    const top = document.createElement('div');
    top.className = 'row top';

    const intro = document.createElement('p');
    intro.textContent = rows.length
      ? 'The API key allows access to all or some systems and talkgroups.'
      : 'No defined API keys.';

    const add = document.createElement('button');
    add.type = 'button';
    add.className = 'rdio-button-mini add';
    add.textContent = 'New API key';
    add.addEventListener('click', () => this.add());

    top.append(intro, add);
    root.append(top);

    const list = document.createElement('div');
    list.className = 'rdio-admin-rows';

    rows.forEach((row, index) => list.append(this.rowElement(row, index, rows)));

    attachDragSort(list, (from, to) => this.commitRows(reorderAfterDrag(this.rows, from, to)));

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

    const errors = apiKeyRowErrors(row, index, rows);

    const header = document.createElement('div');
    header.className = 'row top';
    header.innerHTML = '<i class="material-icons drag-handle" aria-hidden="true">drag_indicator</i>';

    const title = document.createElement('span');
    title.className = 'row-title';
    title.textContent = row.ident || 'NewAPIKey';
    header.append(title);

    if (Object.keys(errors).length) this.setHeaderInvalid(header, true);

    panel.append(header);

    // The row's warning icon follows the row's validity, recomputed from the rows as the section
    // holds them: the row the operator is editing stays on the page, so the icon moves on the spot.
    const refreshMarker = () => {
      const rows = this.rows;

      this.setHeaderInvalid(header, Object.keys(apiKeyRowErrors(rows[index], index, rows)).length > 0);
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

    // The copy button sits inline right of the key input, not on a row of its own.
    const copy = document.createElement('button');
    copy.type = 'button';
    copy.className = 'rdio-icon-button';
    copy.setAttribute('aria-label', 'Copy key to clipboard');
    copy.innerHTML = '<i class="material-icons" aria-hidden="true">content_copy</i>';

    const keyField = this.field('Key', copy);
    const keyInput = keyField.querySelector('input');
    keyInput.value = row.key ?? '';
    keyInput.required = true;

    copy.addEventListener('click', async () => {
      await navigator.clipboard.writeText(keyInput.value);

      showSnackbar('Key copied to clipboard');
    });

    if (errors.key) this.errorTargets.push({ field: keyField, error: errors.key });

    markField(this, keyField, rowKey(row, index), 'key');

    keyInput.addEventListener('change', () => {
      this.updateRow(index, { key: keyInput.value });

      // The message the field carries is the one its row has now, read from the rows as the
      // section holds them: a key another row has is a rule about the whole list.
      const rows = this.rows;

      refreshField(this, keyField, apiKeyRowErrors(rows[index], index, rows).key ?? '');

      refreshMarker();
    });

    const identField = this.field('Ident');
    const identInput = identField.querySelector('input');
    identInput.value = row.ident ?? '';

    if (errors.ident) this.errorTargets.push({ field: identField, error: errors.ident });

    markField(this, identField, rowKey(row, index), 'ident');

    identInput.addEventListener('change', () => {
      this.updateRow(index, { ident: identInput.value });

      const rows = this.rows;

      refreshField(this, identField, apiKeyRowErrors(rows[index], index, rows).ident ?? '');

      refreshMarker();
    });

    const systemsRow = document.createElement('div');
    systemsRow.className = 'row';

    const systemsSummary = document.createElement('p');
    systemsSummary.textContent = `The API key allows access to ${row.systems === '*' ? 'all' : 'some'} systems and talkgroups.`;

    const systemsButton = document.createElement('button');
    systemsButton.type = 'button';
    systemsButton.className = 'rdio-button-mini choose-systems';
    systemsButton.textContent = 'Choose systems';
    systemsButton.addEventListener('click', async () => {
      const value = await openSystemsSelectionDialog({ config: selectionConfig(), value: row.systems ?? '*' });

      if (value === null) return;

      this.commitRows(this.rows.map((r, i) => (i === index ? { ...r, systems: value } : r)));
    });

    systemsRow.append(systemsSummary, systemsButton);

    if (errors.systems) systemsButton.setAttribute('aria-invalid', 'true');

    const bottom = document.createElement('div');
    bottom.className = 'row bottom';

    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'rdio-button-mini delete warn';
    remove.textContent = 'Delete API key';
    remove.addEventListener('click', () => {
      this.commitRows(this.rows.filter((_, i) => i !== index));
    });

    bottom.append(remove);

    panel.append(disabledRow, keyField, identField, systemsRow, bottom);

    return panel;
  }

  field(label, trailing) {
    const field = document.createElement('rdio-form-field');

    const labelEl = document.createElement('label');
    labelEl.textContent = label;

    const input = document.createElement('input');
    input.type = 'text';

    if (trailing) {
      const inline = document.createElement('div');
      inline.className = 'field-inline';
      inline.append(input, trailing);

      field.append(labelEl, inline);
    } else {
      field.append(labelEl, input);
    }

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
    this.commitRows(this.rows.map((row, i) => (i === index ? { ...row, ...patch } : row)), true);
  }

  // Every commit of this section's rows carries the section's validity: the Save control answers
  // the rows the section holds, and adding, deleting, moving a row, and picking its systems all
  // change them.
  commitRows(rows, selfEdit = false) {
    if (selfEdit) this.internalEdit = rows;

    setSectionWorking(NAME, rows);
    setSectionInvalid(NAME, rows.some((row, i) => Object.keys(apiKeyRowErrors(row, i, rows)).length > 0));
  }

  add() {
    // Legacy generates a v4-shaped UUID seeded from the timestamp; the
    // standard crypto.randomUUID() produces the same shape.
    this.commitRows([
      markRowIdentity({ id: 0, order: 0, disabled: false, key: crypto.randomUUID(), ident: '', systems: '*' }),
      ...this.rows.map((row, i) => ({ ...row, order: i + 2 })),
    ]);
  }
}

customElements.define('rdio-admin-apikeys', RdioAdminApikeys);
