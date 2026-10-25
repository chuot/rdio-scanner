// rdio-admin-apikeys: API keys section; collapsible panels, one row open at a time
// the key is generated on add; hidden id/order round-trip; Save lives in the bar.

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
import { showSnackbar } from '../../../../common/frontend/presentation/ui/primitives.js';

const NAME = 'apikeys';

// the prefix keys this section's panels: the open row follows the remembered row,
// not render order.
const PANEL_PREFIX = 'api';

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
    const errors = apiKeyRowErrors(row, index, rows);

    // the panel key names the row itself: the open row follows it, not the position a
    // render happens to leave it in.
    const key = `${PANEL_PREFIX}:${rowKey(row, index)}`;

    const panel = makePanel(this.openState, key, row.ident || 'NewAPIKey', Object.keys(errors).length, { index, total: rows.length });

    // the row's warning icon recomputes from the section's rows: the row stays put
    // and the icon moves in place.
    const refreshMarker = () => {
      const rows = this.rows;

      setRowMarker(panel, Object.keys(apiKeyRowErrors(rows[index], index, rows)).length > 0);
    };

    const disabledRow = document.createElement('div');
    disabledRow.className = 'row';

    const disabledLabel = document.createElement('span');
    disabledLabel.textContent = 'Disabled';

    const disabledToggle = document.createElement('rdio-toggle');
    disabledToggle.checked = !!row.disabled;
    disabledToggle.setAttribute('aria-label', 'Disabled');
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

    const keyField = this.field('Key', copy, true);
    const keyInput = keyField.querySelector('input');
    keyInput.value = row.key ?? '';
    keyInput.required = true;

    copy.addEventListener('click', async () => {
      await navigator.clipboard.writeText(keyInput.value);

      showSnackbar('Key copied to clipboard');
    });

    if (errors.key) this.errorTargets.push({ field: keyField, error: errors.key });

    markField(this, keyField, rowKey(row, index), 'key');

    onFieldEdit(keyInput, () => {
      this.updateRow(index, { key: keyInput.value });

      // the field's message is its row's current one from the section's rows: duplicates
      // are a whole-list rule.
      const rows = this.rows;

      refreshField(this, keyField, apiKeyRowErrors(rows[index], index, rows).key ?? '');

      refreshMarker();
    });

    const identField = this.field('Ident', null, true);
    const identInput = identField.querySelector('input');
    identInput.value = row.ident ?? '';

    if (errors.ident) this.errorTargets.push({ field: identField, error: errors.ident });

    markField(this, identField, rowKey(row, index), 'ident');

    onFieldEdit(identInput, () => {
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

  field(label, trailing, required) {
    const field = document.createElement('rdio-form-field');

    const labelEl = document.createElement('label');
    labelEl.textContent = label;

    const input = document.createElement('input');
    input.type = 'text';

    // the required marker rides the field's own name, with the announced pair on the
    // host; it declares that this section cannot leave the field empty.
    if (required) markRequired(field, labelEl);

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

  updateRow(index, patch) {
    this.commitRows(this.rows.map((row, i) => (i === index ? { ...row, ...patch } : row)), true);
  }

  // every rows commit carries the section's validity: Save answers its rows.
  commitRows(rows, selfEdit = false) {
    if (selfEdit) this.internalEdit = rows;

    setSectionWorking(NAME, rows);
    setSectionInvalid(NAME, rows.some((row, i) => Object.keys(apiKeyRowErrors(row, i, rows)).length > 0));
  }

  add() {
    // keys are generated with the standard crypto.randomUUID().
    const fresh = markRowIdentity({ id: 0, order: 0, disabled: false, key: crypto.randomUUID(), ident: '', systems: '*' });
    const rows = [fresh, ...this.rows.map((row, i) => ({ ...row, order: i + 2 }))];

    // the added row opens as the section's one open row: the others close in the map
    // and on the page, since the next render reads open rows off the page
    // see the single-open-row-rule maintainer note.
    for (const row of this.querySelectorAll('details[data-key]')) row.open = false;

    this.openState.clear();
    this.openState.set(`${PANEL_PREFIX}:${rowKey(fresh, 0)}`, true);

    this.commitRows(rows);
  }
}

customElements.define('rdio-admin-apikeys', RdioAdminApikeys);
