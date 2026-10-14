// rdio-admin-systems: systems section (legacy config/systems).
// The system level only: sites, talkgroups and units are saved on their own in
// the per-level sections, so the section submits and saves the fields of the system row and leaves
// every level to the section that shows it. Validation lives in domain/systems-validation.js.

import { playAlert } from './play-alert.js';
import { blacklistsEntries, removeBlacklistsEntry } from '../../domain/blacklists.js';
import { systemErrors, systemsSectionInvalid } from '../../domain/systems-validation.js';
import { getAdminContext } from '../../application/admin-context.js';
import {
  attachDragSort,
  bindSectionSlice,
  reorderAfterDrag,
  sectionSlice,
  sectionWorking,
  setSectionInvalid,
  setSectionWorking,
  sortedByOrder,
} from './section-shell.js';
import {
  deleteRow,
  markRowIdentity,
  numberField,
  onFieldEdit,
  parseNumber,
  rowContext,
  rowKey,
  selectField,
  setRowMarker,
  textareaField,
  textField,
  titleCase,
  toggleField,
  makePanel,
} from './row-builders.js';
import { clearReached, harvestReached, refreshField, restoreReached } from './sub-section-shell.js';

const NAME = 'systems';

const LEDS = [
  ['', 'Default'],
  ['blue', 'Blue'],
  ['cyan', 'Cyan'],
  ['green', 'Green'],
  ['magenta', 'Magenta'],
  ['orange', 'Orange'],
  ['red', 'Red'],
  ['white', 'White'],
  ['yellow', 'Yellow'],
];

const TYPES = [
  ['', 'No Type'],
  ['am', 'AM'],
  ['dmr', 'DMR'],
  ['fm', 'FM'],
  ['nfm', 'NFM'],
  ['nxdn', 'NXDN'],
  ['p25', 'P25'],
  ['provoice', 'Provoice'],
  ['smartnet', 'SmartNet'],
  ['tetra', 'Tetra'],
];

const TYPE_CAPTION = [
  ['AM', 'Amplitude modulation.'],
  ['DMR', 'Digital mobile radio system.'],
  ['FM', 'Frequency modulation.'],
  ['NFM', 'Narrow frequency modulation.'],
  ['NXDN', 'Next Generation Digital Narrowband radio system.'],
  ['P25', 'APCO-25 radio system.'],
  ['Provoice', 'EDACS Provoice radio system.'],
  ['SmartNet', 'Motorola SmartNet radio system.'],
  ['Tetra', 'Trans european trunked radio system.'],
];

const ALERT_CAPTION = 'Prefixes an alert sound to all calls assigned from this system. The alert assignment order is as follows: Group -> Tag -> System -> Talkgroup.';
const LED_CAPTION = 'The default LED color is green but it can be changed at several levels. The color assignment order is as follows: Group -> Tag -> System -> Talkgroup.';

class RdioAdminSystems extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    this.openState = new Map();
    this.reached = new Set();

    // The systems section renders one slice, so every field key it builds carries that slice's key.
    this.rowKeyPrefix = `section:${NAME}`;

    this.unsubscribe = bindSectionSlices(() => this.render());

    this.render();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
  }

  get systems() {
    return sortedByOrder(sectionWorking(NAME));
  }

  render() {
    const slice = sectionSlice(NAME);

    if (!slice || slice.working === null || slice.working === this.internalEdit) return;

    for (const details of this.querySelectorAll('details[data-key]')) {
      this.openState.set(details.dataset.key, details.open);
    }

    harvestReached(this);

    // A section whose working copy matches the server's baseline — saved, discarded, or pushed
    // from the server — starts with every field unreached: the operator has touched nothing.
    if (!slice.dirty) clearReached(this, this.rowKeyPrefix);

    const systems = this.systems;

    const alerts = getAdminContext().store.getState().alerts ?? {};

    this.errorTargets = [];

    const root = document.createElement('div');
    root.className = 'rdio-admin-card systems';

    const top = document.createElement('div');
    top.className = 'row top';

    const intro = document.createElement('p');
    intro.textContent = 'This is where you define all your systems. Audio files to unknown systems/talkgroups won\'t be ingested unless the auto populate option is activated. You can also drag and drop systems to rearrange their order. The sites, talkgroups and units of a system are saved in their own sections.';

    const add = document.createElement('button');
    add.type = 'button';
    add.className = 'rdio-button-mini add';
    add.textContent = 'New system';
    add.addEventListener('click', () => this.addSystem());

    top.append(intro, add);
    root.append(top);

    const list = document.createElement('div');
    list.className = 'rdio-admin-rows';

    systems.forEach((system, index) => list.append(this.systemElement(system, index, systems, alerts)));

    attachDragSort(list, (from, to) => this.commit(reorderAfterDrag(this.systems, from, to), false));

    root.append(list);

    const bar = document.createElement('rdio-admin-section-bar');
    bar.dataset.section = NAME;
    root.append(bar);

    this.replaceChildren(root);

    for (const { field, error } of this.errorTargets) field.setError(error);

    restoreReached(this);

    setSectionInvalid(NAME, systemsSectionInvalid(systems, sectionWorking('groups') ?? [], sectionWorking('tags') ?? []));
  }

  // Model commit: field edits keep the rendered DOM (internalEdit guard);
  // structural changes (add/delete/drag) re-render. Invalid flag is always
  // recomputed from the committed value.
  commit(rows, selfEdit) {
    if (selfEdit) this.internalEdit = rows;

    setSectionWorking(NAME, rows);
    setSectionInvalid(NAME, systemsSectionInvalid(rows, sectionWorking('groups') ?? [], sectionWorking('tags') ?? []));
  }

  systemElement(system, index, systems, alerts) {
    // The row's errors, recomputed from the rows as they are at the moment of the reading: a
    // duplicate identifier is a rule about the whole list, and the row the operator's value collides
    // with has to be found in the list as it now is.
    const errorsFor = (list) => systemErrors(list[index], list);

    const errors = errorsFor(systems);

    const key = `sys:${rowKey(system, index)}`;
    const ctx = rowContext(this, key);

    const details = makePanel(this.openState, key, system.label?.trim() || 'NewSystem', Object.keys(errors).length);

    const update = (patch, field, name) => {
      const next = this.systems.map((s, i) => (i === index ? { ...s, ...patch } : s));

      this.commit(next, true);

      const nextErrors = errorsFor(next);

      if (field) refreshField(this, field, nextErrors[name] ?? '');

      setRowMarker(details, Object.keys(nextErrors).length > 0);
    };

    const refField = numberField(ctx, details, 'systemRef', 'Id', 'System identifier in decimal format.', system.systemRef, errors.systemRef, true);
    refField.input.min = '1';
    refField.input.step = '1';
    onFieldEdit(refField.input, () => update({ systemRef: parseNumber(refField.input.value) }, refField.field, 'systemRef'));

    const labelField = textField(ctx, details, 'label', 'Label', 'System label displayed on the main screen and on the search panel.', system.label, errors.label, true);
    onFieldEdit(labelField.input, () => update({ label: labelField.input.value }, labelField.field, 'label'));

    const alertOptions = [['', 'No Alert'], ...Object.keys(alerts).map((alert) => [alert, titleCase(alert)])];

    selectField(ctx, details, 'alert', 'Audio alert', ALERT_CAPTION, alertOptions, system.alert ?? '', (value) => {
      update({ alert: value });

      if (value) playAlert(alerts, value);
    });

    toggleField(details, 'Auto Populate', 'Allows the automatic creation of unconfigured talkgroups.', !!system.autoPopulate, (checked) => update({ autoPopulate: checked }));

    const delayField = numberField(ctx, details, 'delay', 'Delay', 'Delay in minutes before propagating calls to clients.', system.delay);
    delayField.input.min = '0';
    delayField.input.step = '1';
    delayField.input.placeholder = 'Minutes';
    delayField.input.addEventListener('change', () => update({ delay: parseNumber(delayField.input.value) }, delayField.field, 'delay'));

    selectField(ctx, details, 'led', 'Led Color', LED_CAPTION, LEDS, system.led ?? '', (value) => update({ led: value }));

    const blacklistsField = textareaField(ctx, details, 'blacklists', 'Blacklists', 'A comma separated list of talkgroup references. The server refuses every call on a talkgroup whose reference is in the list. The Blacklist action of the Talkgroups section adds a talkgroup to this list, and saving that section stores it with the talkgroups.', system.blacklists ?? '', errors.blacklists);
    onFieldEdit(blacklistsField.textarea, () => update({ blacklists: blacklistsField.textarea.value }, blacklistsField.field, 'blacklists'));

    details.append(blacklistsEntryList(system, (list) => this.commit(this.systems.map((s, i) => (i === index ? { ...s, blacklists: list } : s)), false)));

    selectField(ctx, details, 'type', 'Type', null, TYPES, system.type ?? '', (value) => update({ type: value }), TYPE_CAPTION);

    const bottom = document.createElement('div');
    bottom.className = 'row bottom';

    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'rdio-button-mini delete warn';
    remove.textContent = 'Delete system';
    remove.addEventListener('click', () => {
      this.commit(this.systems.filter((_, i) => i !== index), false);
    });

    bottom.append(remove);
    details.append(bottom);

    return details;
  }

  addSystem() {
    const rows = [newSystemRow(), ...this.systems.map((row, i) => ({ ...row, order: i + 2 }))];

    this.commit(rows, false);
  }
}

customElements.define('rdio-admin-systems', RdioAdminSystems);

// The system's blacklist, named. The field holds the references the server refuses, and a bare
// reference tells the operator little: the talkgroup it belongs to is the name they know. Each entry
// is named by the system's talkgroup with that reference, and by the reference alone when the system
// has no talkgroup for it — the reference stays refused either way, and the row the Blacklist action
// removed is in that case exactly the entry the operator wants to read.
function blacklistsEntryList(system, removeEntry) {
  const box = document.createElement('div');
  box.className = 'rdio-admin-blacklist-entries';

  for (const entry of blacklistsEntries(system.blacklists)) {
    const talkgroup = (system.talkgroups ?? []).find((candidate) => String(candidate.talkgroupRef) === entry);

    const row = document.createElement('div');
    row.className = 'rdio-admin-blacklist-entry';

    const label = document.createElement('span');
    label.className = 'rdio-admin-blacklist-entry-label';
    label.textContent = talkgroup?.label?.trim() ? talkgroup.label : entry;

    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'rdio-button-mini';
    remove.textContent = 'Remove';
    remove.addEventListener('click', () => removeEntry(removeBlacklistsEntry(system.blacklists, entry)));

    row.append(label, remove);
    box.append(row);
  }

  return box;
}

function bindSectionSlices(callback) {
  const { store } = getAdminContext();

  return store.subscribe([`section:${NAME}`, 'section:groups', 'section:tags', 'alerts'], callback);
}

function newSystemRow() {
  return markRowIdentity({
    id: 0,
    order: 0,
    alert: '',
    autoPopulate: false,
    blacklists: '',
    delay: null,
    label: '',
    led: '',
    systemRef: null,
    sites: [],
    talkgroups: [],
    type: '',
    units: [],
  });
}
