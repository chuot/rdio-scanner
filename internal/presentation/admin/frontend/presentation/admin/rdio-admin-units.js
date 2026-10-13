// rdio-admin-units: the units of one system, saved on their own.
// Same scaffold as the sites section: a system dropdown, the rows of the
// selected system, add/delete/drag-sort, and the per-level save bar.

import { getAdminContext } from '../../application/admin-context.js';
import { unitErrors } from '../../domain/systems-validation.js';
import { commitSubRows, connectSubSection, disconnectSubSection, refreshField, renderSubSection, rowPanelKey } from './sub-section-shell.js';
import { deleteRow, numberField, onFieldEdit, parseNumber, rowContext, setRowMarker, textField, makePanel } from './row-builders.js';

const LEVEL = 'units';

const CAPTION = 'Create units to display the label instead of showing the unit id on the main screen. You can drag and drop them to rearrange. Pick the system the units belong to.';

const SPEC = { level: LEVEL, caption: CAPTION, addLabel: 'New unit', rowPrefix: 'unit', listen: [], buildRow: unitRow };

function freshRows(component) {
  return getAdminContext().store.getState()[`subsection:${LEVEL}:${component.systemId}`]?.working ?? [];
}

function unitRow(component, unit, index, rows) {
  // The row's errors, recomputed from the rows as they are at the moment of the reading: a
  // duplicate identifier is a rule about the whole list, and the row the operator's value collides
  // with has to be found in the list as it now is.
  const errorsFor = (list) => unitErrors(list[index], list);

  const errors = errorsFor(rows);

  const key = rowPanelKey(component, unit, index);
  const ctx = rowContext(component, key);

  const details = makePanel(component.openState, key, unit.label?.trim() || 'NewUnit', Object.keys(errors).length);

  const update = (patch, field, name) => {
    const next = freshRows(component).map((u, i) => (i === index ? { ...u, ...patch } : u));

    commitSubRows(component, next, true);

    const nextErrors = errorsFor(next);

    if (field) refreshField(component, field, nextErrors[name] ?? '');

    setRowMarker(details, Object.keys(nextErrors).length > 0);
  };

  const refField = numberField(ctx, details, 'unitRef', 'Id', 'Unit identifier in decimal format. You can define a unique unit identifier here.', unit.unitRef, errors.unitRef);
  refField.input.min = '1';
  refField.input.step = '1';
  onFieldEdit(refField.input, () => update({ unitRef: parseNumber(refField.input.value) }, refField.field, 'unitRef'));

  const fromField = numberField(ctx, details, 'unitFrom', 'Range from', 'Unit identifier range from.', unit.unitFrom, errors.unitFrom);
  fromField.input.min = '1';
  fromField.input.step = '1';
  fromField.input.placeholder = 'From';
  onFieldEdit(fromField.input, () => update({ unitFrom: parseNumber(fromField.input.value) }, fromField.field, 'unitFrom'));

  const toField = numberField(ctx, details, 'unitTo', 'Range to', 'Unit identifier range to.', unit.unitTo, errors.unitTo);
  toField.input.min = '1';
  toField.input.step = '1';
  toField.input.placeholder = 'To';
  onFieldEdit(toField.input, () => update({ unitTo: parseNumber(toField.input.value) }, toField.field, 'unitTo'));

  const labelField = textField(ctx, details, 'label', 'Label', 'Unit label.', unit.label, errors.label);
  onFieldEdit(labelField.input, () => update({ label: labelField.input.value }, labelField.field, 'label'));

  deleteRow(details, 'Delete unit', () => {
    commitSubRows(component, freshRows(component).filter((_, i) => i !== index));
  });

  return details;
}

class RdioAdminUnits extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    connectSubSection(this, SPEC);
  }

  disconnectedCallback() {
    disconnectSubSection(this);
  }

  render() {
    renderSubSection(this);
  }
}

customElements.define('rdio-admin-units', RdioAdminUnits);
