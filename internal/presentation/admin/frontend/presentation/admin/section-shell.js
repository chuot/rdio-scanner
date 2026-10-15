// Shared admin section scaffolding (Phase E): a per-section save bar bound to
// the `section:<name>` store slice (Save enabled only when dirty and valid,
// conflict banner with reload, Discard), plus list-ordering helpers for the
// native HTML5 drag sorting (legacy CDK drop(): 1-based order rewrite).

import { getAdminContext } from '../../application/admin-context.js';
import { announce, showSnackbar } from '../../../../common/frontend/presentation/ui/primitives.js';

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

// The system's blacklist, edited by the talkgroups section beside its rows and saved with them.
export function blacklistsSlice(systemId) {
  return getAdminContext().store.getState()[`blacklists:${systemId}`];
}

export function bindBlacklistsSlice(systemId, callback) {
  return getAdminContext().store.subscribe([`blacklists:${systemId}`], () => callback(blacklistsSlice(systemId)));
}

export function blacklistsWorking(systemId) {
  return blacklistsSlice(systemId)?.working ?? '';
}

export function setBlacklistsWorking(systemId, value) {
  getAdminContext().useCases.setBlacklistsWorking(systemId, value);
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

      // The talkgroups level is saved together with the system's blacklist, so its bar answers the
      // two as the one change the operator is standing in.
      if (level === 'talkgroups') this.unsubscribeBlacklists = bindBlacklistsSlice(this.systemId, () => this.render());
    } else {
      this.unsubscribe = bindSectionSlice(this.dataset.section, () => this.render());
    }

    this.render();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
    this.unsubscribeBlacklists?.();
  }

  render() {
    const name = this.dataset.section;

    const slice = this.level ? subSlice(this.level, this.systemId) : sectionSlice(name);

    if (!slice) return;

    const blacklists = this.level === 'talkgroups' ? blacklistsSlice(this.systemId) : null;

    const dirty = slice.dirty || blacklists?.dirty === true;
    const invalid = slice.invalid || blacklists?.invalid === true;
    const conflict = slice.conflict || blacklists?.conflict === true;

    // The system is named by its reference number, the number the operator reads in the section and
    // types into the fields. The scaffold that resolves the shown system hands it to the bar; a bar
    // built without it falls back to the identity the store gave the row.
    const label = this.level
      ? `the ${this.level} of system ${this.dataset.systemRef || this.systemId}`
      : sectionLabel(name);

    const bar = document.createElement('div');
    bar.className = 'rdio-admin-section-bar';

    if (conflict) {
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
    save.disabled = !dirty || invalid;
    save.addEventListener('click', async () => {
      const ok = this.level
        ? await getAdminContext().useCases.saveSubSection(this.level, this.systemId)
        : await getAdminContext().useCases.saveSection(name);

      // The save bar's answer is the operator's question, asked of the server: the polite region
      // says it in words, for the operator who is not looking at the rows when the answer lands.
      if (!ok) {
        showSnackbar(`Unable to save ${label}`);
        announce(`Save failed: ${label}`);
      } else {
        announce('Saved');
      }
    });

    const discard = document.createElement('button');
    discard.type = 'button';
    discard.className = 'rdio-button-mini discard';
    discard.textContent = 'Discard';
    discard.disabled = !dirty && !conflict;
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

// The move a row makes says what it moved and where it landed: the row's own words, and its place
// in the list the listener plays in. The keyboard's Move buttons and the pointer's drag land in
// the same reorder, so the operator who cannot see the list shuffling hears the same sentence from
// either route.
function announceMove(label, index, total) {
  announce(`${label}, position ${index + 1} of ${total}`);
}

// Native HTML5 drag sorting for a container whose direct children carry
// [data-drag] + draggable. onReorder(from, to) receives display indices.
export function attachDragSort(container, onReorder) {
  let fromIndex = null;

  const rows = () => [...container.querySelectorAll(':scope > [data-drag]')];

  // The reorder both routes go through, with the sentence the page says about it: the row's own
  // words, and the place it landed on. The keyboard's Move buttons and the pointer's drag land
  // here, so the operator who cannot see the list shuffling hears the move from either route.
  const move = (from, to) => {
    const label = rows()[from]?.querySelector('.row-title')?.textContent.trim() ?? '';

    onReorder(from, to);

    announceMove(label, to, rows().length);
  };

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

    if (to !== -1 && to !== from) move(from, to);
  });

  container.addEventListener('dragend', () => {
    fromIndex = null;

    for (const row of rows()) row.classList.remove('dragging');
  });

  // The keyboard's route to the same reorder: the Move up and Move down buttons a row carries beside
  // its handle. The click the keyboard's Enter and Space leave on a button lands here, and the move
  // goes through the same onReorder the drop goes through, so the order the save bar sends is the
  // same order either way.
  //
  // The buttons sit inside the row's own control — the summary line that opens and closes the row —
  // so the click that moves the row takes that activation away from the browser: moving a row must
  // not open it.
  container.addEventListener('click', (ev) => {
    const button = ev.target?.closest?.('[data-move]');

    if (!button || button.disabled) return;

    ev.preventDefault();

    const from = rows().indexOf(button.closest('[data-drag]'));
    const to = button.dataset.move === 'up' ? from - 1 : from + 1;

    if (from === -1 || to < 0 || to >= rows().length) return;

    move(from, to);
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
