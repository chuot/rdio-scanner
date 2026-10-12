// rdio-admin-tags: tags section (legacy config/tags). Identical shape
// to the groups section: drag-sortable panels with label, audio alert select
// with audition, led color select. New Tag inserts first; hidden id/order
// round-trip; per-section Save lives in the shared bar.

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

const NAME = 'tags';

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

    attachDragSort(list, (from, to) => setSectionWorking(NAME, reorderAfterDrag(rows, from, to)));

    root.append(list);

    const bar = document.createElement('rdio-admin-section-bar');
    bar.dataset.section = NAME;
    root.append(bar);

    this.replaceChildren(root);

    for (const { field, message } of errorFields) field.setError(message);
  }

  rowElement(row, index, alerts, errorFields) {
    const panel = document.createElement('div');
    panel.className = 'rdio-admin-row';
    panel.dataset.drag = '';
    panel.draggable = true;

    const errors = tagRowErrors(row);

    const header = document.createElement('div');
    header.className = 'row top';
    header.innerHTML = '<i class="material-icons drag-handle" aria-hidden="true">drag_indicator</i>';

    const title = document.createElement('span');
    title.className = 'row-title';
    title.textContent = row.label || 'NewTag';
    header.append(title);

    if (errors.label) this.setRowInvalid(header, true);

    panel.append(header);

    // label
    const labelRow = this.descriptionRow('Label', 'Tag label displayed on the main screen and on the search panel.');

    const labelField = document.createElement('rdio-form-field');
    const labelInput = document.createElement('input');
    labelInput.type = 'text';
    labelInput.value = row.label ?? '';
    labelInput.required = true;
    labelInput.setAttribute('aria-label', 'Label');
    labelInput.addEventListener('change', () => {
      this.updateRow(index, { label: labelInput.value });
      labelField.setError(labelInput.value ? '' : 'Label is required');
      title.textContent = labelInput.value || 'NewTag';
      this.setRowInvalid(header, !labelInput.value);
    });
    labelField.append(labelInput);
    labelRow.append(labelField);
    panel.append(labelRow);

    if (errors.label) errorFields.push({ field: labelField, message: errors.label });

    // alert — select with audition (legacy playAlert on selectionChange).
    const alertRow = this.descriptionRow('Audio alert', 'Prefixes an alert sound to all calls assigned to this tag. The alert assignment order is as follows: Group -> Tag -> System -> Talkgroup.');

    const alertField = document.createElement('rdio-form-field');
    const alertSelect = document.createElement('rdio-select');
    alertSelect.setAttribute('options', JSON.stringify([['', 'No Alert'], ...Object.keys(alerts).map((name) => [name, titleCase(name)])]));
    alertSelect.setAttribute('value', row.alert ?? '');
    alertSelect.setAttribute('aria-label', 'Alert');
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
    ledSelect.setAttribute('aria-label', 'Color');
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
      setSectionWorking(NAME, this.rows.filter((_, i) => i !== index));
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

  setRowInvalid(header, invalid) {
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
    setSectionInvalid(NAME, rows.some((row) => !row.label));
  }

  add() {
    const rows = [{ alert: '', label: '', led: '', order: 0 }, ...this.rows];

    setSectionWorking(NAME, rows);
    setSectionInvalid(NAME, rows.some((row) => !row.label));
  }
}

customElements.define('rdio-admin-tags', RdioAdminTags);
