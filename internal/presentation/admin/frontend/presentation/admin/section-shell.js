// Shared admin section scaffolding (Phase E): a per-section save bar bound to
// the `section:<name>` store slice (Save enabled only when dirty and valid,
// conflict banner with reload, Discard), plus list-ordering helpers for the
// native HTML5 drag sorting (legacy CDK drop(): 1-based order rewrite).

import { getAdminContext } from '../../application/admin-context.js';
import { showSnackbar } from '../../../../common/frontend/presentation/ui/primitives.js';

const SECTION_LABELS = {
  access: 'Access',
  apikeys: 'API keys',
  dirwatch: 'Dirwatch',
  downstreams: 'Downstreams',
  groups: 'Groups',
  options: 'Options',
  systems: 'Systems',
  tags: 'Tags',
};

export function sectionLabel(name) {
  return SECTION_LABELS[name] ?? name;
}

export function sectionSlice(name) {
  return getAdminContext().store.getState()[`section:${name}`];
}

export function bindSectionSlice(name, callback) {
  return getAdminContext().store.subscribe([`section:${name}`], () => callback(sectionSlice(name)));
}

// Current editable copy of the section; sections always edit through this.
export function sectionWorking(name) {
  return sectionSlice(name)?.working;
}

// Config for the systems-selection dialog: the working copies of the
// groups, tags and systems sections.
export function selectionConfig() {
  return {
    groups: sectionWorking('groups') ?? [],
    tags: sectionWorking('tags') ?? [],
    systems: sectionWorking('systems') ?? [],
  };
}

export function setSectionWorking(name, value) {
  getAdminContext().useCases.setSectionWorking(name, value);
}

export function setSectionInvalid(name, invalid) {
  getAdminContext().useCases.setSectionInvalid(name, invalid);
}

// Per-level slices: `subsection:<level>:<systemId>` carries the same
// baseline/working/dirty/invalid/conflict state a section slice does.
export function subSlice(level, systemId) {
  return getAdminContext().store.getState()[`subsection:${level}:${systemId}`];
}

export function bindSubSlice(level, systemId, callback) {
  return getAdminContext().store.subscribe([`subsection:${level}:${systemId}`], () => callback(subSlice(level, systemId)));
}

export function subWorking(level, systemId) {
  return subSlice(level, systemId)?.working;
}

export function setSubWorking(level, systemId, value) {
  getAdminContext().useCases.setSubSectionWorking(level, systemId, value);
}

export function setSubInvalid(level, systemId, invalid) {
  getAdminContext().useCases.setSubSectionInvalid(level, systemId, invalid);
}

class RdioAdminSectionBar extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    // A bar bound to one level of one system carries `data-subsection`
    // ("<level>:<systemId>") instead of `data-section`.
    if (this.dataset.subsection) {
      const [level, systemId] = this.dataset.subsection.split(':');

      this.level = level;
      this.systemId = Number(systemId);

      this.unsubscribe = bindSubSlice(level, this.systemId, () => this.render());
    } else {
      this.unsubscribe = bindSectionSlice(this.dataset.section, () => this.render());
    }

    this.render();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
  }

  render() {
    const name = this.dataset.section;

    const slice = this.level ? subSlice(this.level, this.systemId) : sectionSlice(name);

    if (!slice) return;

    const label = this.level ? `the ${this.level} of system ${this.systemId}` : sectionLabel(name);

    const bar = document.createElement('div');
    bar.className = 'rdio-admin-section-bar';

    if (slice.conflict) {
      const banner = document.createElement('div');
      banner.className = 'rdio-admin-conflict';
      banner.setAttribute('role', 'alert');

      const message = document.createElement('span');
      message.textContent = 'The server changed this section while you were editing it.';

      const reload = document.createElement('button');
      reload.type = 'button';
      reload.className = 'rdio-button-mini';
      reload.textContent = 'Reload from server';
      reload.addEventListener('click', () =>
        this.level ? getAdminContext().useCases.reloadSubSection(this.level, this.systemId) : getAdminContext().useCases.reloadSection(name));

      banner.append(message, reload);
      bar.append(banner);
    }

    const save = document.createElement('button');
    save.type = 'button';
    save.className = 'rdio-button save';
    save.textContent = 'Save';
    save.disabled = !slice.dirty || slice.invalid;
    save.addEventListener('click', async () => {
      const ok = this.level
        ? await getAdminContext().useCases.saveSubSection(this.level, this.systemId)
        : await getAdminContext().useCases.saveSection(name);

      if (!ok) showSnackbar(`Unable to save ${label}`);
    });

    const discard = document.createElement('button');
    discard.type = 'button';
    discard.className = 'rdio-button-mini discard';
    discard.textContent = 'Discard';
    discard.disabled = !slice.dirty && !slice.conflict;
    discard.addEventListener('click', () =>
      this.level ? getAdminContext().useCases.discardSubSection(this.level, this.systemId) : getAdminContext().useCases.discardSection(name));

    bar.append(save, discard);

    this.replaceChildren(bar);
  }
}

customElements.define('rdio-admin-section-bar', RdioAdminSectionBar);

// Display order: `order` ascending, missing order last (legacy `|| 0`).
export function sortedByOrder(list) {
  return [...(list ?? [])].sort((a, b) => (a.order || 0) - (b.order || 0));
}

// Native HTML5 drag sorting for a container whose direct children carry
// [data-drag] + draggable. onReorder(from, to) receives display indices.
export function attachDragSort(container, onReorder) {
  let fromIndex = null;

  const rows = () => [...container.querySelectorAll(':scope > [data-drag]')];

  container.addEventListener('dragstart', (ev) => {
    const row = ev.target.closest('[data-drag]');

    if (!row) return;

    fromIndex = rows().indexOf(row);

    ev.dataTransfer.effectAllowed = 'move';

    row.classList.add('dragging');
  });

  container.addEventListener('dragover', (ev) => {
    if (fromIndex === null) return;

    ev.preventDefault();

    ev.dataTransfer.dropEffect = 'move';
  });

  container.addEventListener('drop', (ev) => {
    if (fromIndex === null) return;

    ev.preventDefault();

    const from = fromIndex;
    const row = ev.target.closest('[data-drag]');
    const to = row ? rows().indexOf(row) : -1;

    fromIndex = null;

    row?.classList.remove('dragging');

    if (to !== -1 && to !== from) onReorder(from, to);
  });

  container.addEventListener('dragend', () => {
    fromIndex = null;

    for (const row of rows()) row.classList.remove('dragging');
  });
}

// Legacy drop(): move the dropped row inside the sorted list, then rewrite
// every row's order to its 1-based index.
export function reorderAfterDrag(list, fromIndex, toIndex) {
  const sorted = sortedByOrder(list);

  const [item] = sorted.splice(fromIndex, 1);

  sorted.splice(toIndex, 0, item);

  return sorted.map((moved, index) => ({ ...moved, order: index + 1 }));
}
