// rdio-admin-units (FEAT-016): the units of one system, saved on their own.
// Same scaffold as the sites section: a system dropdown, the rows of the
// selected system, add/delete/drag-sort, and the per-level save bar.

import { getAdminContext } from '../../application/admin-context.js';
import { unitErrors } from '../../domain/systems-validation.js';
import { setSubWorking } from './section-shell.js';
import { renderSubSection } from './sub-section-shell.js';
import { deleteRow, numberField, parseNumber, rowKey, textField, makePanel } from './row-builders.js';

const LEVEL = 'units';

const CAPTION = 'Create units to display the label instead of showing the unit id on the main screen. You can drag and drop them to rearrange. Pick the system the units belong to.';

function unitRow(component, unit, index, rows) {
  const errors = unitErrors(unit, rows);

  const key = `unit:${rowKey(unit, index)}`;

  const details = makePanel(component.openState, key, unit.label?.trim() || 'NewUnit', Object.keys(errors).length);

  const update = (patch) => {
    const fresh = getAdminContext().store.getState()[`subsection:${LEVEL}:${component.systemId}`]?.working ?? [];

    setSubWorking(LEVEL, component.systemId, fresh.map((u, i) => (i === index ? { ...u, ...patch } : u)));
  };

  const refField = numberField(component.errorTargets, details, 'Id', 'Unit identifier in decimal format. You can define a unique unit identifier here.', unit.unitRef, errors.unitRef);
  refField.input.min = '1';
  refField.input.step = '1';
  refField.input.addEventListener('change', () => update({ unitRef: parseNumber(refField.input.value) }));

  const fromField = numberField(component.errorTargets, details, 'Range from', 'Unit identifier range from.', unit.unitFrom, errors.unitFrom);
  fromField.input.min = '1';
  fromField.input.step = '1';
  fromField.input.placeholder = 'From';
  fromField.input.addEventListener('change', () => update({ unitFrom: parseNumber(fromField.input.value) }));

  const toField = numberField(component.errorTargets, details, 'Range to', 'Unit identifier range to.', unit.unitTo, errors.unitTo);
  toField.input.min = '1';
  toField.input.step = '1';
  toField.input.placeholder = 'To';
  toField.input.addEventListener('change', () => update({ unitTo: parseNumber(fromField.input.value) }));

  const labelField = textField(component.errorTargets, details, 'Label', 'Unit label.', unit.label, errors.label);
  labelField.input.addEventListener('change', () => update({ label: labelField.input.value }));

  deleteRow(details, 'Delete unit', () => {
    const fresh = getAdminContext().store.getState()[`subsection:${LEVEL}:${component.systemId}`]?.working ?? [];

    setSubWorking(LEVEL, component.systemId, fresh.filter((_, i) => i !== index));
  });

  return details;
}

class RdioAdminUnits extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    this.openState = new Map();

    this.rebind();

    this.render();
  }

  rebind() {
    for (const unsubscribe of this.unsubscribes ?? []) unsubscribe();

    const { store } = getAdminContext();

    this.unsubscribes = [store.subscribe(['section:systems', `subsection:${LEVEL}:${this.systemId}`], () => this.render())];
  }

  disconnectedCallback() {
    for (const unsubscribe of this.unsubscribes ?? []) unsubscribe();
  }

  render() {
    for (const details of this.querySelectorAll('details[data-key]')) {
      this.openState.set(details.dataset.key, details.open);
    }

    renderSubSection(this, { level: LEVEL, caption: CAPTION, addLabel: 'New unit', buildRow: unitRow });
  }
}

customElements.define('rdio-admin-units', RdioAdminUnits);
