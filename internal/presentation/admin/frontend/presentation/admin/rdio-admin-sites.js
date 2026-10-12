// rdio-admin-sites: the sites of one system, saved on their own.
// A system dropdown picks the system; the rows are added, edited, deleted and
// drag-sorted in the `subsection:sites:<systemId>` slice, and the save bar
// issues the per-level PUT only.

import { getAdminContext } from '../../application/admin-context.js';
import { siteErrors } from '../../domain/systems-validation.js';
import { setSubWorking } from './section-shell.js';
import { renderSubSection } from './sub-section-shell.js';
import { deleteRow, numberField, parseNumber, rowKey, textField, makePanel } from './row-builders.js';

const LEVEL = 'sites';

const CAPTION = 'System sites, you can drag and drop sites to rearrange them. Pick the system the sites belong to.';

function siteRow(component, site, index, rows) {
  const errors = siteErrors(site, rows);

  const key = `site:${rowKey(site, index)}`;

  const details = makePanel(component.openState, key, site.label?.trim() || 'NewSite', Object.keys(errors).length);

  const update = (patch) => {
    const fresh = getAdminContext().store.getState()[`subsection:${LEVEL}:${component.systemId}`]?.working ?? [];

    setSubWorking(LEVEL, component.systemId, fresh.map((s, i) => (i === index ? { ...s, ...patch } : s)));
  };

  const refField = numberField(component.errorTargets, details, 'Id', 'Site identifier in decimal format.', site.siteRef, errors.siteRef);
  refField.input.min = '1';
  refField.input.step = '1';
  refField.input.addEventListener('change', () => update({ siteRef: parseNumber(refField.input.value) }));

  const labelField = textField(component.errorTargets, details, 'Label', 'Site label.', site.label, errors.label);
  labelField.input.addEventListener('change', () => update({ label: labelField.input.value }));

  deleteRow(details, 'Delete site', () => {
    const fresh = getAdminContext().store.getState()[`subsection:${LEVEL}:${component.systemId}`]?.working ?? [];

    setSubWorking(LEVEL, component.systemId, fresh.filter((_, i) => i !== index));
  });

  return details;
}

class RdioAdminSites extends HTMLElement {
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

    renderSubSection(this, { level: LEVEL, caption: CAPTION, addLabel: 'New Site', buildRow: siteRow });
  }
}

customElements.define('rdio-admin-sites', RdioAdminSites);
