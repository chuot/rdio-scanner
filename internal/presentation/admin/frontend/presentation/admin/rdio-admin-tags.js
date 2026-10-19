// rdio-admin-tags: tags section (legacy config/tags). Identical shape
// to the groups section: collapsible panels, one row open at a time, and
// drag-sortable, with label, audio alert select with audition, led color
// select. New Tag inserts first and opens alone; hidden id/order round-trip;
// per-section Save lives in the shared bar.

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

// The rows of this section carry its prefix on the panel, the way systems-tree rows carry theirs:
// the open row follows the row the section remembers, not the place a render leaves it in.
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

    // The fields of this section's working copy remember the state of having been reached, the way
    // the systems tree sections do: the key they are remembered by names this section's slice.
    this.reached = new Set();
    this.rowKeyPrefix = `section:${NAME}`;

    // Which rows of this section the operator holds open, carried across the rebuilds the store
    // causes, exactly as the systems tree carries its open row.
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

    // The rows the operator left open ride the rebuild: the page is the state the operator reads,
    // and the section's map is what reproduces it, so the open rows are read off the page before
    // the rows are built again.
    for (const details of this.querySelectorAll('details[data-key]')) {
      this.openState.set(details.dataset.key, details.open);
    }

    harvestReached(this);

    // A section whose working copy matches the server's baseline — saved, discarded, or pushed from
    // the server — starts with every field unreached: the operator has touched nothing.
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

    // One row open at a time: the rows the operator is not working on shrink, so the list stays
    // short and the save bar under it is reached without scrolling past the rows opened on the way.
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

    // The row is remembered on its panel by a key of its own, the way a systems-tree row is:
    // the open row follows the row the operator works on, not the place a render leaves it in.
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

    // alert — select with audition (legacy playAlert on selectionChange).
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

  // Every commit of this section's rows carries the section's validity: the Save control answers the
  // rows the section holds, and adding, deleting, and moving a row change them. The move reads the
  // rows as the store holds them at the moment of the drop, so a value the operator typed in a row
  // that is deliberately not rebuilt is not left behind.
  commitRows(rows, selfEdit = false) {
    if (selfEdit) this.internalEdit = rows;

    setSectionWorking(NAME, rows);
    setSectionInvalid(NAME, rows.some((row) => Object.keys(tagRowErrors(row)).length > 0));
  }

  add() {
    const fresh = markRowIdentity({ alert: '', label: '', led: '', order: 0 });

    // The row the add control creates is the row the operator came to fill in, so it opens, and it
    // is the section's one open row. The rows the operator left open close on the page as well as
    // in the map: the render that follows reads the open rows off the page, and a row left open
    // there would come back open.
    for (const row of this.querySelectorAll('details[data-key]')) row.open = false;

    this.openState.clear();
    this.openState.set(`${PANEL_PREFIX}:${rowKey(fresh, 0)}`, true);

    this.commitRows([fresh, ...this.rows]);
  }
}

customElements.define('rdio-admin-tags', RdioAdminTags);
