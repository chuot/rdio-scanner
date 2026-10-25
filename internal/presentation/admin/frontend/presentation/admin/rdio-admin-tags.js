// rdio-admin-tags: tags section, same shape as groups: collapsible panels, one row open
// at a time, drag-sortable; New Tag inserts first, opens alone; hidden id/order round-trip

import { getAdminContext } from '../../application/admin-context.js';
import { playAlert } from './play-alert.js';
import {
  attachDragSort,
  reorderAfterDrag,
  sectionSlice,
  sectionWorking,
  setSectionInvalid,
  setSectionWorking,
  sortedByOrder,
} from './section-shell.js';
import {
  attachSingleOpenRule,
  makePanel,
  markField,
  markRequired,
  markRowIdentity,
  onFieldEdit,
  rowKey,
  setRowMarker,
  setRowMoveLabels,
} from './row-builders.js';
import { clearReached, harvestReached, restoreReached } from './sub-section-shell.js';

const NAME = 'tags';

// the tag panel prefix keys open state: the open row follows what the section remembers
const PANEL_PREFIX = 'tag';

const LEDS = ['blue', 'cyan', 'green', 'magenta', 'orange', 'red', 'white', 'yellow'];

export function titleCase(value) {
  return String(value)
    .toLowerCase()
    .replace(/(^|\s)([a-z])/g, (_, space, char) => space + char.toUpperCase());
}

export function tagRowErrors(row) {
  const errors = {};

  if (!row.label) errors.label = 'Label is required';

  return errors;
}

class RdioAdminTags extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    const { store } = getAdminContext();

    // reached fields of the tags working copy, keyed by the section:tags slice
    this.reached = new Set();
    this.rowKeyPrefix = `section:${NAME}`;

    // openState holds the operator's open rows across the store's rebuilds
    this.openState = new Map();

    this.unsubscribe = store.subscribe([`section:${NAME}`, 'alerts'], () => this.render());

    this.render();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
  }

  get rows() {
    return sortedByOrder(sectionWorking(NAME));
  }

  get alerts() {
    return getAdminContext().store.getState().alerts ?? {};
  }

  render() {
    const slice = sectionSlice(NAME);

    if (!slice || slice.working === this.internalEdit) return;

    // open rows are read off the page into the map just before the rows are rebuilt
    for (const details of this.querySelectorAll('details[data-key]')) {
      this.openState.set(details.dataset.key, details.open);
    }

    harvestReached(this);

    // a tags working copy matching the server baseline leaves every field unreached
    if (!slice.dirty) clearReached(this, this.rowKeyPrefix);

    const rows = this.rows;
    const alerts = this.alerts;
    const errorFields = [];

    const root = document.createElement('div');
    root.className = 'rdio-admin-card tags';

    const top = document.createElement('div');
    top.className = 'row top';

    const intro = document.createElement('p');
    intro.textContent = 'All system talkgroups must be associated with a tag, which is then used to search for calls based on their tag.';

    const add = document.createElement('button');
    add.type = 'button';
    add.className = 'rdio-button-mini add';
    add.textContent = 'New Tag';
    add.addEventListener('click', () => this.add());

    top.append(intro, add);
    root.append(top);

    if (!rows.length) {
      const empty = document.createElement('p');
      empty.className = 'rdio-admin-empty';
      empty.textContent = 'No defined Tags';
      root.append(empty);
    }

    const list = document.createElement('div');
    list.className = 'rdio-admin-rows';

    rows.forEach((row, index) => list.append(this.rowElement(row, index, alerts, errorFields)));

    attachDragSort(list, (from, to) => this.commitRows(reorderAfterDrag(this.rows, from, to)));

    // one open row keeps the tags list short: the save bar below needs no scrolling
    attachSingleOpenRule(list, this.openState);

    root.append(list);

    const bar = document.createElement('rdio-admin-section-bar');
    bar.dataset.section = NAME;
    root.append(bar);

    this.replaceChildren(root);

    for (const { field, message } of errorFields) field.setError(message);

    restoreReached(this);
  }

  rowElement(row, index, alerts, errorFields) {
    const errors = tagRowErrors(row);

    // tag:key panel key: the open row follows the operator's row, not the render place
    const key = `${PANEL_PREFIX}:${rowKey(row, index)}`;

    const panel = makePanel(this.openState, key, row.label || 'NewTag', Object.keys(errors).length, { index, total: this.rows.length });

    const title = panel.querySelector('.row-title');

    // label
    const labelRow = this.descriptionRow('Label', 'Tag label displayed on the main screen and on the search panel.');

    const labelField = document.createElement('rdio-form-field');
    const labelInput = document.createElement('input');
    labelInput.type = 'text';
    labelInput.value = row.label ?? '';
    labelInput.required = true;
    labelInput.setAttribute('aria-label', 'Label');
    onFieldEdit(labelInput, () => {
      this.updateRow(index, { label: labelInput.value });
      labelField.setError(labelInput.value ? '' : 'Label is required');
      title.textContent = labelInput.value || 'NewTag';
      setRowMoveLabels(panel, title.textContent);
      setRowMarker(panel, !labelInput.value);
    });
    labelField.append(labelInput);
    labelRow.append(labelField);
    panel.append(labelRow);

    markField(this, labelField, rowKey(row, index), 'label');
    markRequired(labelField, labelRow.querySelector('.field-title'));

    if (errors.label) errorFields.push({ field: labelField, message: errors.label });

    // audio alert select, audition through playAlert
    const alertRow = this.descriptionRow('Audio alert', 'Prefixes an alert sound to all calls assigned to this tag. The alert assignment order is as follows: Group -> Tag -> System -> Talkgroup.');

    const alertField = document.createElement('rdio-form-field');
    const alertSelect = document.createElement('rdio-select');
    alertSelect.setAttribute('options', JSON.stringify([['', 'No Alert'], ...Object.keys(alerts).map((name) => [name, titleCase(name)])]));
    alertSelect.setAttribute('value', row.alert ?? '');
    alertSelect.setAttribute('aria-label', 'Audio alert');
    alertSelect.addEventListener('change', (ev) => {
      // ignore the bubbled native change (null/undefined detail); only the
      // primitive's CustomEvent carries the string value.
      if (typeof ev.detail !== 'string') return;

      this.updateRow(index, { alert: ev.detail });
      playAlert(this.alerts, ev.detail);
    });
    alertField.append(alertSelect);
    alertRow.append(alertField);
    panel.append(alertRow);

    // led
    const ledRow = this.descriptionRow('Led Color', 'The default LED color is green but it can be changed at several levels. The color assignment order is as follows: Group -> Tag -> System -> Talkgroup.');

    const ledField = document.createElement('rdio-form-field');
    const ledSelect = document.createElement('rdio-select');
    ledSelect.setAttribute('options', JSON.stringify([['', 'Default'], ...LEDS.map((led) => [led, titleCase(led)])]));
    ledSelect.setAttribute('value', row.led ?? '');
    ledSelect.setAttribute('aria-label', 'Led Color');
    ledSelect.addEventListener('change', (ev) => {
      if (typeof ev.detail === 'string') this.updateRow(index, { led: ev.detail });
    });
    ledField.append(ledSelect);
    ledRow.append(ledField);
    panel.append(ledRow);

    const bottom = document.createElement('div');
    bottom.className = 'row bottom';

    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'rdio-button-mini delete warn';
    remove.textContent = 'Delete Tag';
    remove.addEventListener('click', () => {
      this.commitRows(this.rows.filter((_, i) => i !== index));
    });

    bottom.append(remove);
    panel.append(bottom);

    return panel;
  }

  descriptionRow(title, text) {
    const row = document.createElement('div');
    row.className = 'row';

    const description = document.createElement('p');
    description.innerHTML = '<span class="field-title"></span><br><span class="field-caption"></span>';
    description.querySelector('.field-title').textContent = title;
    description.querySelector('.field-caption').textContent = text;

    row.append(description);

    return row;
  }

  updateRow(index, patch) {
    this.commitRows(this.rows.map((row, i) => (i === index ? { ...row, ...patch } : row)), true);
  }

  // every rows commit carries section validity for the Save control
  // a drop re-reads the rows the store holds, not typed values left in rows not rebuilt
  // see the rows-commit-validity maintainer note.
  commitRows(rows, selfEdit = false) {
    if (selfEdit) this.internalEdit = rows;

    setSectionWorking(NAME, rows);
    setSectionInvalid(NAME, rows.some((row) => Object.keys(tagRowErrors(row)).length > 0));
  }

  add() {
    const fresh = markRowIdentity({ alert: '', label: '', led: '', order: 0 });

    // the added row opens as the section's one open row
    // open details close on the page too: the next render reads open rows from the DOM
    // see the open-row-dom-reset maintainer note.
    for (const row of this.querySelectorAll('details[data-key]')) row.open = false;

    this.openState.clear();
    this.openState.set(`${PANEL_PREFIX}:${rowKey(fresh, 0)}`, true);

    this.commitRows([fresh, ...this.rows]);
  }
}

customElements.define('rdio-admin-tags', RdioAdminTags);
