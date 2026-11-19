// rdio-admin-access: access codes section; collapsible panels, one row open at a
// time; hidden id/order round-trip; per-section Save lives in the shared bar.

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
import { attachSingleOpenRule, makePanel, markField, markRequired, markRowIdentity, onFieldEdit, rowKey, setRowMarker } from './row-builders.js';
import { clearReached, harvestReached, refreshField, restoreReached } from './sub-section-shell.js';
import { generateAccessCode } from '../../domain/random-generator.js';
import { announce } from '../../../../common/frontend/presentation/ui/primitives.js';

const NAME = 'access';

// the prefix keys this section's panels: the open row follows the remembered row,
// not render order.
const PANEL_PREFIX = 'acc';

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

    // reached fields of this section's working copy; the key names its slice.
    this.reached = new Set();
    this.rowKeyPrefix = `section:${NAME}`;

    // the rows the operator holds open, kept across the rebuilds the store causes.
    this.openState = new Map();

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

    // open rows are read off the page before the rebuild: the map reproduces what the
    // operator reads.
    for (const details of this.querySelectorAll('details[data-key]')) {
      this.openState.set(details.dataset.key, details.open);
    }

    harvestReached(this);

    // a clean section (saved, discarded, pushed) starts with every field unreached.
    if (!slice.dirty) clearReached(this, this.rowKeyPrefix);

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

    attachDragSort(list, (from, to) => this.commitRows(reorderAfterDrag(this.rows, from, to)));

    // one row open at a time: the list stays short and the save bar stays reachable.
    attachSingleOpenRule(list, this.openState);

    root.append(list);

    const bar = document.createElement('rdio-admin-section-bar');
    bar.dataset.section = NAME;
    root.append(bar);

    this.replaceChildren(root);

    for (const { field, error } of this.errorTargets) field.setError(error);

    restoreReached(this);
  }

  rowElement(row, index, rows) {
    const errors = accessRowErrors(row, index, rows);

    // the panel key names the row itself: the open row follows it, not the position a
    // render happens to leave it in.
    const key = `${PANEL_PREFIX}:${rowKey(row, index)}`;

    const panel = makePanel(this.openState, key, row.ident || 'NewAccess', Object.keys(errors).length, { index, total: rows.length });

    // the row's warning icon recomputes from the section's rows: the row stays put
    // and the icon moves in place.
    const refreshMarker = () => {
      const rows = this.rows;

      setRowMarker(panel, Object.keys(accessRowErrors(rows[index], index, rows)).length > 0);
    };

    // the code field is hidden by default, with a visibility toggle and a control that draws a new code.
    const reveal = document.createElement('button');
    reveal.type = 'button';
    reveal.className = 'rdio-icon-button';

    // the toggle's words, its mark, and its pressed state are one state, written in one place: a control
    // that says "Show code" while the code is already showing names the move it would undo, not the one it
    // now makes.
    const paintReveal = () => {
      const showing = codeInput.type === 'text';

      reveal.setAttribute('aria-pressed', String(showing));
      reveal.setAttribute('aria-label', showing ? 'Hide code' : 'Show code');
      reveal.innerHTML = `<i class="material-icons" aria-hidden="true">${showing ? 'visibility_off' : 'visibility'}</i>`;
    };

    reveal.addEventListener('click', () => {
      codeInput.type = codeInput.type === 'text' ? 'password' : 'text';

      paintReveal();
    });

    const generate = document.createElement('button');
    generate.type = 'button';
    generate.className = 'rdio-icon-button';
    generate.setAttribute('aria-label', 'Generate new code');
    generate.innerHTML = '<i class="material-icons" aria-hidden="true">autorenew</i>';
    generate.addEventListener('click', () => {
      // the codes the section's other rows hold: writing one of them would only be refused at save, so the
      // generator draws again instead.
      const held = this.rows.filter((_, i) => i !== index).map((other) => other.code);

      const code = generateAccessCode(globalThis.crypto, held);

      if (code === null) {
        announce('Could not generate an access code.');

        return;
      }

      this.updateRow(index, { code });

      // the code the operator is meant to hand to someone is a code they can read: the draw leaves the
      // field showing it, and the toggle says so.
      codeInput.value = code;
      codeInput.type = 'text';

      paintReveal();

      // the message and the warning follow the value that landed: a duplicate message beside a code that
      // is not one is a warning the operator has to second-guess.
      const rows = this.rows;

      refreshField(this, codeField, accessRowErrors(rows[index], index, rows).code ?? '');

      refreshMarker();

      announce('New access code generated. Save applies it.');
    });

    const codeField = this.field('Code', [reveal, generate], true);
    const codeInput = codeField.querySelector('input');
    codeInput.type = 'password';
    codeInput.value = row.code ?? '';
    codeInput.required = true;

    paintReveal();

    this.bindText(codeInput, codeField, index, 'code', errors.code, refreshMarker);

    const identField = this.field('Ident', null, true);
    identField.querySelector('input').value = row.ident ?? '';
    this.bindText(identField.querySelector('input'), identField, index, 'ident', errors.ident, refreshMarker);

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

    // systems — selection dialog hookup.
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

      this.commitRows(this.rows.map((r, i) => (i === index ? { ...r, systems: value } : r)));
    });
    systemsRow.append(systemsSummary, systemsButton);

    const bottom = document.createElement('div');
    bottom.className = 'row bottom';

    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'rdio-button-mini delete warn';
    remove.textContent = 'Delete access';
    remove.addEventListener('click', () => {
      this.commitRows(this.rows.filter((_, i) => i !== index));
    });

    bottom.append(remove);

    panel.append(codeField, identField, expirationField, limitField, systemsRow, bottom);

    if (errors.systems) systemsButton.setAttribute('aria-invalid', 'true');

    return panel;
  }

  // a field's controls: the input, and when the field carries controls beside it an inline row holding the
  // input first and the controls to its right, in the order they are handed in.
  field(label, trailing, required) {
    const field = document.createElement('rdio-form-field');

    const labelEl = document.createElement('label');
    labelEl.textContent = label;

    const input = document.createElement('input');
    input.type = 'text';

    // the required marker rides the field's own name, with the announced pair on the
    // host; it declares that this section cannot leave the field empty.
    if (required) markRequired(field, labelEl);

    if (trailing?.length) {
      const inline = document.createElement('div');
      inline.className = 'field-inline';
      inline.append(input, ...trailing);

      field.append(labelEl, inline);
    } else {
      field.append(labelEl, input);
    }

    return field;
  }

  bindText(input, field, index, key, error, refreshMarker) {
    if (error) this.errorTargets.push({ field, error });

    markField(this, field, rowKey(this.rows[index], index), key);

    // index (not row identity): updateRow replaces the row object with a
    // patched copy, so a captured row would no longer be found in the store.
    onFieldEdit(input, () => {
      this.updateRow(index, { [key]: input.value });

      // the field's message is its row's current one from the section's rows: duplicates
      // are a whole-list rule.
      const rows = this.rows;

      refreshField(this, field, accessRowErrors(rows[index], index, rows)[key] ?? '');

      refreshMarker();
    });
  }

  updateRow(index, patch) {
    this.commitRows(this.rows.map((row, i) => (i === index ? { ...row, ...patch } : row)), true);
  }

  // every rows commit carries the section's validity: Save answers its rows.
  commitRows(rows, selfEdit = false) {
    if (selfEdit) this.internalEdit = rows;

    setSectionWorking(NAME, rows);
    setSectionInvalid(NAME, rows.some((row, i) => Object.keys(accessRowErrors(row, i, rows)).length > 0));
  }

  add() {
    const fresh = markRowIdentity({ code: '', ident: '', systems: '*', order: 0 });
    const rows = [fresh, ...this.rows.map((row, i) => ({ ...row, order: i + 2 }))];

    // the added row opens as the section's one open row: the others close in the map
    // and on the page, since the next render reads open rows off the page.
    for (const row of this.querySelectorAll('details[data-key]')) row.open = false;

    this.openState.clear();
    this.openState.set(`${PANEL_PREFIX}:${rowKey(fresh, 0)}`, true);

    this.commitRows(rows);
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
