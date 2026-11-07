// rdio-admin-downstreams: downstream connections as collapsible row panels,
// one open at a time: disabled, url, apikey, systems selection.

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

const NAME = 'downstreams';

// 'dwn' prefix: downstreams panels follow the row they remember, not render order
const PANEL_PREFIX = 'dwn';

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

    // reached fields are remembered under keys naming the downstreams slice
    this.reached = new Set();
    this.rowKeyPrefix = `section:${NAME}`;

    // openState: which downstreams rows stay open across store rebuilds
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

    // open rows are read off the downstreams page before rows are built again,
    // and reproduced from the section's openState map
    for (const details of this.querySelectorAll('details[data-key]')) {
      this.openState.set(details.dataset.key, details.open);
    }

    harvestReached(this);

    // a clean downstreams slice starts with every field unreached
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

    attachDragSort(list, (from, to) => this.commitRows(reorderAfterDrag(this.rows, from, to)));

    // one downstreams row open at a time keeps the list short
    // and the save bar reachable without scrolling past open rows
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
    const errors = downstreamRowErrors(row, index, rows);

    // the downstreams panel key from PANEL_PREFIX and rowKey follows the row itself
    const key = `${PANEL_PREFIX}:${rowKey(row, index)}`;

    const panel = makePanel(this.openState, key, row.url || 'NewDownstream', Object.keys(errors).length, { index, total: rows.length });

    // the downstreams row's warning icon tracks its validity via refreshMarker,
    // recomputed from this.rows since the edited row stays on the page
    const refreshMarker = () => {
      const rows = this.rows;

      setRowMarker(panel, Object.keys(downstreamRowErrors(rows[index], index, rows)).length > 0);
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

    const urlField = this.field('URL', true);
    const urlInput = urlField.querySelector('input');
    urlInput.value = row.url ?? '';
    urlInput.required = true;

    if (errors.url) this.errorTargets.push({ field: urlField, error: errors.url });

    markField(this, urlField, rowKey(row, index), 'url');

    onFieldEdit(urlInput, () => {
      this.updateRow(index, { url: urlInput.value });

      // the url field's message comes from the current rows the section holds:
      // a URL duplicated on another downstreams row is a whole-list rule
      const rows = this.rows;

      refreshField(this, urlField, downstreamRowErrors(rows[index], index, rows).url ?? '');

      refreshMarker();
    });

    const apikeyField = this.field('API key', true);
    const apikeyInput = apikeyField.querySelector('input');
    apikeyInput.value = row.apikey ?? '';
    apikeyInput.required = true;

    if (errors.apikey) this.errorTargets.push({ field: apikeyField, error: errors.apikey });

    markField(this, apikeyField, rowKey(row, index), 'apikey');

    onFieldEdit(apikeyInput, () => {
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

      this.commitRows(this.rows.map((r, i) => (i === index ? { ...r, systems: value } : r)));
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
      this.commitRows(this.rows.filter((_, i) => i !== index));
    });

    bottom.append(remove);

    panel.append(disabledRow, urlField, apikeyField, systemsRow, bottom);

    return panel;
  }

  field(label, required) {
    const field = document.createElement('rdio-form-field');

    const labelEl = document.createElement('label');
    labelEl.textContent = label;

    const input = document.createElement('input');
    input.type = 'text';

    // the downstreams asterisk says the field cannot be empty: it rides the name,
    // and the field host carries the other half for the control to announce it
    if (required) markRequired(field, labelEl);

    field.append(labelEl, input);

    return field;
  }

  updateRow(index, patch) {
    this.commitRows(this.rows.map((row, i) => (i === index ? { ...row, ...patch } : row)), true);
  }

  // every downstreams commit carries section validity to the Save control:
  // add, delete, move and systems picks change the rows it checks
  commitRows(rows, selfEdit = false) {
    if (selfEdit) this.internalEdit = rows;

    setSectionWorking(NAME, rows);
    setSectionInvalid(NAME, rows.some((row, i) => Object.keys(downstreamRowErrors(row, i, rows)).length > 0));
  }

  add() {
    const fresh = markRowIdentity({ id: 0, order: 0, apikey: '', disabled: false, systems: '*', url: '' });
    const rows = [fresh, ...this.rows.map((row, i) => ({ ...row, order: i + 2 }))];

    // the added downstreams row opens for filling in as the section's one open row;
    // rows left open on the page close too: the next render reads them off the page
    for (const row of this.querySelectorAll('details[data-key]')) row.open = false;

    this.openState.clear();
    this.openState.set(`${PANEL_PREFIX}:${rowKey(fresh, 0)}`, true);

    this.commitRows(rows);
  }
}

customElements.define('rdio-admin-downstreams', RdioAdminDownstreams);
