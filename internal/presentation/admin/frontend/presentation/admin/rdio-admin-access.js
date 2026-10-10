// rdio-admin-access (T037): access codes section (legacy config/access).
// Rows: code (hide/show), ident, expiration, limit, systems selection dialog.
// Hidden id/order round-trip; per-section Save lives in the shared bar.

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

const NAME = 'access';

export function accessRowErrors(row, index, rows) {
  const errors = {};

  if (!row.code) errors.code = 'Code is required';
  else if (rows.some((other, i) => i !== index && other.code === row.code)) errors.code = 'Code is already defined';

  if (!row.ident) errors.ident = 'Ident is required';

  if (row.systems === undefined || row.systems === null) errors.systems = 'Systems is required';

  return errors;
}

class RdioAdminAccess extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    this.unsubscribe = bindSectionSlice(NAME, () => this.render());

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

    const rows = this.rows;

    // rdio-form-field.setError only exists after the field is connected;
    // errors are collected during construction and applied after render.
    this.errorTargets = [];

    const root = document.createElement('div');
    root.className = 'rdio-admin-card access';

    const top = document.createElement('div');
    top.className = 'row top';

    const intro = document.createElement('p');
    intro.textContent = rows.length
      ? 'The access code allows access to all or some systems and talkgroups.'
      : 'No access is defined, your instance is open and accessible to all.';

    const add = document.createElement('button');
    add.type = 'button';
    add.className = 'rdio-button-mini add';
    add.textContent = 'New access';
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
  }

  rowElement(row, index, rows) {
    const panel = document.createElement('div');
    panel.className = 'rdio-admin-row';
    panel.dataset.drag = '';
    panel.draggable = true;

    const errors = accessRowErrors(row, index, rows);

    const header = document.createElement('div');
    header.className = 'row top';
    header.innerHTML = `<i class="material-icons drag-handle" aria-hidden="true">drag_indicator</i>`;

    const title = document.createElement('span');
    title.className = 'row-title';
    title.textContent = row.ident || 'NewAccess';
    header.append(title);

    if (Object.keys(errors).length) {
      const icon = document.createElement('i');
      icon.className = 'material-icons row-invalid';
      icon.setAttribute('aria-label', 'Invalid row');
      icon.textContent = 'error';
      header.append(icon);
    }

    panel.append(header);

    // code — hidden by default with a visibility toggle (legacy hideCode). The
    // toggle sits inline right of the input, not on a row of its own.
    const reveal = document.createElement('button');
    reveal.type = 'button';
    reveal.className = 'rdio-icon-button';
    reveal.setAttribute('aria-label', 'Show code');
    reveal.innerHTML = '<i class="material-icons" aria-hidden="true">visibility</i>';
    reveal.addEventListener('click', () => {
      const hidden = codeInput.type === 'password';
      codeInput.type = hidden ? 'text' : 'password';
      reveal.innerHTML = `<i class="material-icons" aria-hidden="true">${hidden ? 'visibility_off' : 'visibility'}</i>`;
    });

    const codeField = this.field('Code', reveal);
    const codeInput = codeField.querySelector('input');
    codeInput.type = 'password';
    codeInput.value = row.code ?? '';
    codeInput.required = true;

    this.bindText(codeInput, codeField, index, 'code', errors.code);

    const identField = this.field('Ident');
    identField.querySelector('input').value = row.ident ?? '';
    this.bindText(identField.querySelector('input'), identField, index, 'ident', errors.ident);

    const expirationField = this.field('Expiration');
    const expirationInput = expirationField.querySelector('input');
    expirationInput.type = 'datetime-local';
    expirationInput.value = toLocalInputValue(row.expiration);
    expirationInput.addEventListener('change', () => {
      this.updateRow(index, { expiration: expirationInput.value ? new Date(expirationInput.value).toISOString() : null });
    });

    const limitField = this.field('Limit');
    const limitInput = limitField.querySelector('input');
    limitInput.type = 'number';
    limitInput.min = '0';
    limitInput.step = '1';
    limitInput.value = row.limit ?? '';
    limitInput.addEventListener('change', () => {
      this.updateRow(index, { limit: limitInput.value === '' ? undefined : Number(limitInput.value) });
    });

    // systems — selection dialog hookup (T036).
    const systemsRow = document.createElement('div');
    systemsRow.className = 'row';

    const systemsSummary = document.createElement('p');
    systemsSummary.textContent = `The access code allows access to ${row.systems === '*' ? 'all' : 'some'} systems and talkgroups.`;

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

    const bottom = document.createElement('div');
    bottom.className = 'row bottom';

    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'rdio-button-mini delete warn';
    remove.textContent = 'Delete access';
    remove.addEventListener('click', () => {
      setSectionWorking(NAME, this.rows.filter((_, i) => i !== index));
    });

    bottom.append(remove);

    panel.append(codeField, identField, expirationField, limitField, systemsRow, bottom);

    if (errors.systems) systemsButton.setAttribute('aria-invalid', 'true');

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

  bindText(input, field, index, key, error) {
    if (error) this.errorTargets.push({ field, error });

    // index (not row identity): updateRow replaces the row object with a
    // patched copy, so a captured row would no longer be found in the store.
    input.addEventListener('change', () => {
      this.updateRow(index, { [key]: input.value });
    });
  }

  updateRow(index, patch) {
    const rows = this.rows.map((row, i) => (i === index ? { ...row, ...patch } : row));

    this.internalEdit = rows;

    setSectionWorking(NAME, rows);
    setSectionInvalid(NAME, rows.some((row, i) => Object.keys(accessRowErrors(row, i, rows)).length > 0));
  }

  add() {
    const rows = [{ code: '', ident: '', systems: '*', order: 0 }, ...this.rows.map((row, i) => ({ ...row, order: i + 2 }))];

    setSectionWorking(NAME, rows);
  }
}

customElements.define('rdio-admin-access', RdioAdminAccess);

function toLocalInputValue(iso) {
  if (!iso) return '';

  const date = new Date(iso);

  if (Number.isNaN(date.getTime())) return '';

  const pad = (n) => String(n).padStart(2, '0');

  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
