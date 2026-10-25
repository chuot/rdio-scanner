// rdio-admin-sites: sites of one system picked from a dropdown, edited and drag-sorted in
// the subsection:sites:<systemId> slice; the save bar issues the per-level PUT only

import { getAdminContext } from '../../application/admin-context.js';
import { siteErrors } from '../../domain/systems-validation.js';
import { commitSubRows, connectSubSection, disconnectSubSection, refreshField, renderSubSection, rowPanelKey } from './sub-section-shell.js';
import { deleteRow, numberField, onFieldEdit, parseNumber, rowContext, setRowMarker, textField, makePanel } from './row-builders.js';

const LEVEL = 'sites';

const CAPTION = 'System sites, you can drag and drop sites to rearrange them. Pick the system the sites belong to.';

const SPEC = { level: LEVEL, caption: CAPTION, addLabel: 'New Site', rowPrefix: 'site', listen: [], buildRow: siteRow };

function freshRows(component) {
  return getAdminContext().store.getState()[`subsection:${LEVEL}:${component.systemId}`]?.working ?? [];
}

function siteRow(component, site, index, rows) {
  // errorsFor recomputes siteErrors on the current list: a duplicate identifier check must
  // find the row the value collides with as the list now is
  const errorsFor = (list) => siteErrors(list[index], list);

  const errors = errorsFor(rows);

  const key = rowPanelKey(component, site, index);
  const ctx = rowContext(component, key);

  const details = makePanel(component.openState, key, site.label?.trim() || 'NewSite', Object.keys(errors).length,
    { index, total: rows.length });

  const update = (patch, field, name) => {
    const next = freshRows(component).map((s, i) => (i === index ? { ...s, ...patch } : s));

    commitSubRows(component, next, true);

    const nextErrors = errorsFor(next);

    if (field) refreshField(component, field, nextErrors[name] ?? '');

    setRowMarker(details, Object.keys(nextErrors).length > 0);
  };

  const refField = numberField(ctx, details, 'siteRef', 'Id', 'Site identifier in decimal format.', site.siteRef, errors.siteRef, true);
  refField.input.min = '1';
  refField.input.step = '1';
  onFieldEdit(refField.input, () => update({ siteRef: parseNumber(refField.input.value) }, refField.field, 'siteRef'));

  const labelField = textField(ctx, details, 'label', 'Label', 'Site label.', site.label, errors.label, true);
  onFieldEdit(labelField.input, () => update({ label: labelField.input.value }, labelField.field, 'label'));

  deleteRow(details, 'Delete site', () => {
    commitSubRows(component, freshRows(component).filter((_, i) => i !== index));
  });

  return details;
}

class RdioAdminSites extends HTMLElement {
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

customElements.define('rdio-admin-sites', RdioAdminSites);
