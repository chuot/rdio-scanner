// rdio-admin-systems (T042, FEAT-016): systems section (legacy config/systems).
// The system level only: sites, talkgroups and units are saved on their own in
// the per-level sections, so the nested sub-lists are gone. Validation lives in
// domain/systems-validation.js; the systems section still saves the whole
// systems array, sub-levels included, exactly as the section endpoint does.

import { playAlert } from './play-alert.js';
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
  numberField,
  parseNumber,
  rowKey,
  selectField,
  textareaField,
  textField,
  titleCase,
  toggleField,
  makePanel,
} from './row-builders.js';

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
    const errors = systemErrors(system, systems);

    const key = rowKey(system, index);

    const details = makePanel(this.openState, `sys:${key}`, system.label?.trim() || 'NewSystem', Object.keys(errors).length);

    const update = (patch) => {
      this.commit(this.systems.map((s, i) => (i === index ? { ...s, ...patch } : s)), true);
    };

    const refField = numberField(this.errorTargets, details, 'Id', 'System identifier in decimal format.', system.systemRef, errors.systemRef);
    refField.input.min = '1';
    refField.input.step = '1';
    refField.input.addEventListener('change', () => update({ systemRef: parseNumber(refField.input.value) }));

    const labelField = textField(this.errorTargets, details, 'Label', 'System label displayed on the main screen and on the search panel.', system.label, errors.label);
    labelField.input.addEventListener('change', () => update({ label: labelField.input.value }));

    const alertOptions = [['', 'No Alert'], ...Object.keys(alerts).map((alert) => [alert, titleCase(alert)])];

    selectField(this.errorTargets, details, 'Audio alert', ALERT_CAPTION, alertOptions, system.alert ?? '', (value) => {
      update({ alert: value });

      if (value) playAlert(alerts, value);
    });

    toggleField(details, 'Auto Populate', 'Allows the automatic creation of unconfigured talkgroups.', !!system.autoPopulate, (checked) => update({ autoPopulate: checked }));

    const delayField = numberField(this.errorTargets, details, 'Delay', 'Delay in minutes before propagating calls to clients.', system.delay);
    delayField.input.min = '0';
    delayField.input.step = '1';
    delayField.input.placeholder = 'Minutes';
    delayField.input.addEventListener('change', () => update({ delay: parseNumber(delayField.input.value) }));

    selectField(this.errorTargets, details, 'Led Color', LED_CAPTION, LEDS, system.led ?? '', (value) => update({ led: value }));

    const blacklistsField = textareaField(this.errorTargets, details, 'Blacklists', 'A comma separated list of talkgroup Ids to blacklist from this system. This only has an effect if the auto populate option is enabled.', system.blacklists ?? '', errors.blacklists);
    blacklistsField.textarea.addEventListener('change', () => update({ blacklists: blacklistsField.textarea.value }));

    selectField(this.errorTargets, details, 'Type', null, TYPES, system.type ?? '', (value) => update({ type: value }), TYPE_CAPTION);

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

function bindSectionSlices(callback) {
  const { store } = getAdminContext();

  return store.subscribe([`section:${NAME}`, 'section:groups', 'section:tags', 'alerts'], callback);
}

function newSystemRow() {
  return {
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
  };
}
