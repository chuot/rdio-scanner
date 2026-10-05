// rdio-admin-systems (T042): systems section (legacy config/systems +
// system + site + talkgroup + unit). Four-level tree: systems → sites /
// talkgroups / units, each level drag-sorted with 1-based order rewrite.
// Validation lives in domain/systems-validation.js; talkgroup groupIds and
// tagId options come from the groups/tags working copies (saved rows only,
// foreign keys need real ids). `Blacklist talkgroup` appends the talkgroup
// id to the system blacklists and removes the row, like the legacy tool.

import { playAlert } from './play-alert.js';
import {
  siteErrors,
  systemErrors,
  systemsSectionInvalid,
  talkgroupErrors,
  unitErrors,
} from '../../domain/systems-validation.js';
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
const TALKGROUP_ALERT_CAPTION = 'Prefixes an alert sound to all calls assigned from this talkgroup. The alert assignment order is as follows: Group -> Tag -> System -> Talkgroup.';
const LED_CAPTION = 'The default LED color is green but it can be changed at several levels. The color assignment order is as follows: Group -> Tag -> System -> Talkgroup.';

function titleCase(value) {
  return value.replace(/[_-]/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

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

    const groups = (sectionWorking('groups') ?? []).filter((group) => typeof group.id === 'number' && group.id > 0);
    const tags = (sectionWorking('tags') ?? []).filter((tag) => typeof tag.id === 'number' && tag.id > 0);

    const alerts = getAdminContext().store.getState().alerts ?? {};

    this.errorTargets = [];

    const root = document.createElement('div');
    root.className = 'rdio-admin-card systems';

    const top = document.createElement('div');
    top.className = 'row top';

    const intro = document.createElement('p');
    intro.textContent = 'This is where you define all your systems and talkgroups are defined. Audio files to unknown systems/talkgroups won\'t be ingested unless the auto populate option is activated. You can also drag and drop systems to rearrange their order.';

    const add = document.createElement('button');
    add.type = 'button';
    add.className = 'rdio-button-mini add';
    add.textContent = 'New system';
    add.addEventListener('click', () => this.addSystem());

    top.append(intro, add);
    root.append(top);

    const list = document.createElement('div');
    list.className = 'rdio-admin-rows';

    systems.forEach((system, index) => list.append(this.systemElement(system, index, systems, groups, tags, alerts)));

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

  // --- system level ----------------------------------------------------------

  systemElement(system, index, systems, groups, tags, alerts) {
    const errors = systemErrors(system, systems);

    const key = rowKey(system, index);

    const details = this.panel(`sys:${key}`, system.label?.trim() || 'NewSystem', Object.keys(errors).length);

    const update = (patch) => {
      this.commit(this.systems.map((s, i) => (i === index ? { ...s, ...patch } : s)), true);
    };

    const refField = this.numberField(details, 'Id', 'System identifier in decimal format.', system.systemRef, errors.systemRef);
    refField.input.min = '1';
    refField.input.step = '1';
    refField.input.addEventListener('change', () => update({ systemRef: parseNumber(refField.input.value) }));

    const labelField = this.textField(details, 'Label', 'System label displayed on the main screen and on the search panel.', system.label, errors.label);
    labelField.input.addEventListener('change', () => update({ label: labelField.input.value }));

    const alertOptions = [['', 'No Alert'], ...Object.keys(alerts).map((alert) => [alert, titleCase(alert)])];

    this.selectField(details, 'Audio alert', ALERT_CAPTION, alertOptions, system.alert ?? '', (value) => {
      update({ alert: value });

      if (value) playAlert(alerts, value);
    });

    this.toggleField(details, 'Auto Populate', 'Allows the automatic creation of unconfigured talkgroups.', !!system.autoPopulate, (checked) => update({ autoPopulate: checked }));

    const delayField = this.numberField(details, 'Delay', 'Delay in minutes before propagating calls to clients.', system.delay);
    delayField.input.min = '0';
    delayField.input.step = '1';
    delayField.input.placeholder = 'Minutes';
    delayField.input.addEventListener('change', () => update({ delay: parseNumber(delayField.input.value) }));

    this.selectField(details, 'Led Color', LED_CAPTION, LEDS, system.led ?? '', (value) => update({ led: value }));

    const blacklistsField = this.textareaField(details, 'Blacklists', 'A comma separated list of talkgroup Ids to blacklist from this system. This only has an effect if the auto populate option is enabled.', system.blacklists ?? '', errors.blacklists);
    blacklistsField.textarea.addEventListener('change', () => update({ blacklists: blacklistsField.textarea.value }));

    this.selectField(details, 'Type', null, TYPES, system.type ?? '', (value) => update({ type: value }), TYPE_CAPTION);

    this.subList(details, system, index, 'sites', 'Sites', 'System sites, you can drag and drop sites to rearrange them.', 'New Site', groups, tags, alerts);
    this.subList(details, system, index, 'talkgroups', 'Talkgroups', 'System talkgroups, you can drag and drop talkgroups to rearrange them. They will appear with the same order on the talkgroup selection panel unless the sort talkgroups option is enabled.', 'New talkgroup', groups, tags, alerts);
    this.subList(details, system, index, 'units', 'Units', 'Create units to display the label instead of showing the unit id on the main screen. You can drag and drop them to rearrange', 'New unit', groups, tags, alerts);

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

  // --- sub lists (sites / talkgroups / units) --------------------------------

  subList(details, system, systemIndex, listKey, title, caption, addButtonLabel, groups, tags, alerts) {
    const section = document.createElement('details');
    section.className = 'rdio-admin-sublist';
    section.dataset.key = `${details.dataset.key}:${listKey}`;
    section.open = this.openState.get(section.dataset.key) ?? false;

    const summary = document.createElement('summary');
    summary.textContent = title;
    section.append(summary);

    // Fresh views of the sub list at action time, so handlers never build on
    // a stale render-time array.
    const freshRows = () => (this.systems[systemIndex] ?? {})[listKey] ?? [];

    const updateSystem = (patch, selfEdit) => {
      this.commit(this.systems.map((s, i) => (i === systemIndex ? { ...s, ...patch } : s)), selfEdit);
    };

    const rows = system[listKey] ?? [];

    const top = document.createElement('div');
    top.className = 'row top';

    const help = document.createElement('p');
    help.className = 'caption';
    help.textContent = caption;

    const add = document.createElement('button');
    add.type = 'button';
    add.className = 'rdio-button-mini add';
    add.textContent = addButtonLabel;
    add.addEventListener('click', () => updateSystem({ [listKey]: [newRow(listKey), ...freshRows().map((row, i) => ({ ...row, order: i + 2 }))] }, false));

    top.append(help, add);
    section.append(top);

    const list = document.createElement('div');
    list.className = 'rdio-admin-rows';

    rows.forEach((row, rowIndex) => {
      const element =
        listKey === 'sites'
          ? this.siteElement(row, rowIndex, `${details.dataset.key}:site`, updateSystem, freshRows)
          : listKey === 'talkgroups'
            ? this.talkgroupElement(row, rowIndex, systemIndex, groups, tags, alerts, `${details.dataset.key}:tg`, updateSystem, freshRows)
            : this.unitElement(row, rowIndex, `${details.dataset.key}:unit`, updateSystem, freshRows);

      list.append(element);
    });

    attachDragSort(list, (from, to) => updateSystem({ [listKey]: reorderAfterDrag(freshRows(), from, to) }, false));

    section.append(list);
    details.append(section);
  }

  siteElement(site, index, keyPrefix, updateSystem, fresh) {
    const errors = siteErrors(site, fresh());

    const details = this.panel(`${keyPrefix}:${rowKey(site, index)}`, site.label?.trim() || 'NewSite', Object.keys(errors).length);

    const update = (patch) => updateSystem({ sites: fresh().map((s, i) => (i === index ? { ...s, ...patch } : s)) }, true);

    const refField = this.numberField(details, 'Id', 'Site identifier in decimal format.', site.siteRef, errors.siteRef);
    refField.input.min = '1';
    refField.input.step = '1';
    refField.input.addEventListener('change', () => update({ siteRef: parseNumber(refField.input.value) }));

    const labelField = this.textField(details, 'Label', 'Site label.', site.label, errors.label);
    labelField.input.addEventListener('change', () => update({ label: labelField.input.value }));

    this.deleteRow(details, 'Delete site', () => updateSystem({ sites: fresh().filter((_, i) => i !== index) }, false));

    return details;
  }

  talkgroupElement(talkgroup, index, systemIndex, groups, tags, alerts, keyPrefix, updateSystem, fresh) {
    const errors = talkgroupErrors(talkgroup, fresh(), {
      groupIds: groups.map((group) => group.id),
      tagIds: tags.map((tag) => tag.id),
    });

    const details = this.panel(`${keyPrefix}:${rowKey(talkgroup, index)}`, talkgroup.label?.trim() || 'NewTalkgroup', Object.keys(errors).length);

    const update = (patch) => updateSystem({ talkgroups: fresh().map((t, i) => (i === index ? { ...t, ...patch } : t)) }, true);

    const refField = this.numberField(details, 'Id', 'Talkgroup identifier in decimal format.', talkgroup.talkgroupRef, errors.talkgroupRef);
    refField.input.min = '1';
    refField.input.step = '1';
    refField.input.addEventListener('change', () => update({ talkgroupRef: parseNumber(refField.input.value) }));

    const labelField = this.textField(details, 'Label', 'Talkgroup label displayed on the main screen and on buttons.', talkgroup.label, errors.label);
    labelField.input.addEventListener('change', () => update({ label: labelField.input.value }));

    const nameField = this.textField(details, 'Name', 'Talkgroup name displayed on the main screen.', talkgroup.name, errors.name);
    nameField.input.addEventListener('change', () => update({ name: nameField.input.value }));

    // groupIds — checkbox list of the saved groups (multi-select).
    const groupsRow = document.createElement('div');
    groupsRow.className = 'row';

    const groupsHelp = document.createElement('p');
    groupsHelp.innerHTML = '<span class="body">Groups</span><br><span class="caption">Groups to which this talkgroups belongs.</span>';

    const groupsList = document.createElement('div');
    groupsList.className = 'rdio-checkbox-list';

    const selectedGroupIds = Array.isArray(talkgroup.groupIds) ? talkgroup.groupIds : typeof talkgroup.groupIds === 'number' ? [talkgroup.groupIds] : [];

    for (const group of groups) {
      const label = document.createElement('label');

      const checkbox = document.createElement('input');
      checkbox.type = 'checkbox';
      checkbox.checked = selectedGroupIds.includes(group.id);
      checkbox.addEventListener('change', () => {
        const next = checkbox.checked
          ? [...selectedGroupIds, group.id]
          : selectedGroupIds.filter((id) => id !== group.id);

        update({ groupIds: next });
      });

      const text = document.createElement('span');
      text.textContent = group.label;

      label.append(checkbox, text);
      groupsList.append(label);
    }

    if (errors.groupIds) groupsRow.dataset.invalid = 'true';

    groupsRow.append(groupsHelp, groupsList);
    details.append(groupsRow);

    const tagOptions = tags.map((tag) => [String(tag.id), tag.label]);

    this.selectField(details, 'Tag', 'Tag to which this talkgroups belongs.', tagOptions, talkgroup.tagId === null || talkgroup.tagId === undefined ? '' : String(talkgroup.tagId), (value) => update({ tagId: value === '' ? null : Number(value) }), null, errors.tagId);

    const alertOptions = [['', 'No Alert'], ...Object.keys(alerts).map((alert) => [alert, titleCase(alert)])];

    this.selectField(details, 'Audio alert', TALKGROUP_ALERT_CAPTION, alertOptions, talkgroup.alert ?? '', (value) => {
      update({ alert: value });

      if (value) playAlert(alerts, value);
    });

    const delayField = this.numberField(details, 'Delay', 'Delay in minutes before propagating calls to clients.', talkgroup.delay);
    delayField.input.min = '0';
    delayField.input.step = '1';
    delayField.input.placeholder = 'Minutes';
    delayField.input.addEventListener('change', () => update({ delay: parseNumber(delayField.input.value) }));

    const frequencyField = this.numberField(details, 'Frequency', 'Fake frequency in hertz displayed on the main screen.', talkgroup.frequency, errors.frequency);
    frequencyField.input.min = '0';
    frequencyField.input.addEventListener('change', () => update({ frequency: parseNumber(frequencyField.input.value) }));

    this.selectField(details, 'Led Color', LED_CAPTION, LEDS, talkgroup.led ?? '', (value) => update({ led: value }));

    this.selectField(details, 'Type', 'Type of radio system that will override the type defined at the system level, if any.', TYPES, talkgroup.type ?? '', (value) => update({ type: value }), TYPE_CAPTION);

    const bottom = document.createElement('div');
    bottom.className = 'row bottom';

    if (talkgroup.id) {
      const blacklist = document.createElement('button');
      blacklist.type = 'button';
      blacklist.className = 'rdio-button-mini blacklist';
      blacklist.textContent = 'Blacklist talkgroup';
      blacklist.addEventListener('click', () => {
        const current = this.systems[systemIndex] ?? {};

        const blacklists = current.blacklists?.trim() ? `${current.blacklists},${talkgroup.id}` : `${talkgroup.id}`;

        updateSystem({ blacklists, talkgroups: fresh().filter((_, i) => i !== index) }, false);
      });

      bottom.append(blacklist);
    }

    this.deleteRow(details, 'Delete talkgroup', () => updateSystem({ talkgroups: fresh().filter((_, i) => i !== index) }, false), bottom);

    return details;
  }

  unitElement(unit, index, keyPrefix, updateSystem, fresh) {
    const errors = unitErrors(unit, fresh());

    const details = this.panel(`${keyPrefix}:${rowKey(unit, index)}`, unit.label?.trim() || 'NewUnit', Object.keys(errors).length);

    const update = (patch) => updateSystem({ units: fresh().map((u, i) => (i === index ? { ...u, ...patch } : u)) }, true);

    const refField = this.numberField(details, 'Id', 'Unit identifier in decimal format. You can define a unique unit identifier here.', unit.unitRef, errors.unitRef);
    refField.input.min = '1';
    refField.input.step = '1';
    refField.input.addEventListener('change', () => update({ unitRef: parseNumber(refField.input.value) }));

    const fromField = this.numberField(details, 'Range from', 'Unit identifier range from.', unit.unitFrom, errors.unitFrom);
    fromField.input.min = '1';
    fromField.input.step = '1';
    fromField.input.placeholder = 'From';
    fromField.input.addEventListener('change', () => update({ unitFrom: parseNumber(fromField.input.value) }));

    const toField = this.numberField(details, 'Range to', 'Unit identifier range to.', unit.unitTo, errors.unitTo);
    toField.input.min = '1';
    toField.input.step = '1';
    toField.input.placeholder = 'To';
    toField.input.addEventListener('change', () => update({ unitTo: parseNumber(toField.input.value) }));

    const labelField = this.textField(details, 'Label', 'Unit label.', unit.label, errors.label);
    labelField.input.addEventListener('change', () => update({ label: labelField.input.value }));

    this.deleteRow(details, 'Delete unit', () => updateSystem({ units: fresh().filter((_, i) => i !== index) }, false));

    return details;
  }

  // --- shared builders ---------------------------------------------------------

  panel(key, title, invalid) {
    const details = document.createElement('details');
    details.className = 'rdio-admin-row';
    details.dataset.key = key;
    details.dataset.drag = '';
    details.draggable = true;
    details.open = this.openState.get(key) ?? false;

    const summary = document.createElement('summary');

    const handle = document.createElement('i');
    handle.className = 'material-icons drag-handle';
    handle.setAttribute('aria-hidden', 'true');
    handle.textContent = 'drag_indicator';
    summary.append(handle);

    const titleSpan = document.createElement('span');
    titleSpan.className = 'row-title';
    titleSpan.textContent = title;
    summary.append(titleSpan);

    if (invalid) {
      const icon = document.createElement('i');
      icon.className = 'material-icons row-invalid';
      icon.setAttribute('aria-label', 'Invalid row');
      icon.textContent = 'error';
      summary.append(icon);
    }

    details.append(summary);

    return details;
  }

  captionBlock(title, caption, typeList) {
    const help = document.createElement('p');

    const body = document.createElement('span');
    body.className = 'body';
    body.textContent = title;

    help.append(body, document.createElement('br'));

    if (caption) {
      const captionEl = document.createElement('span');
      captionEl.className = 'caption';
      captionEl.textContent = caption;
      help.append(captionEl);
    }

    if (typeList) {
      const list = document.createElement('ul');

      for (const [name, description] of typeList) {
        const item = document.createElement('li');

        const bold = document.createElement('b');
        bold.textContent = name;

        item.append(bold, ' - ', document.createTextNode(description));
        list.append(item);
      }

      help.append(list);
    }

    return help;
  }

  formField(details, title, caption, control, error, typeList) {
    const row = document.createElement('div');
    row.className = 'row';

    row.append(this.captionBlock(title, caption, typeList));

    const field = document.createElement('rdio-form-field');

    const label = document.createElement('label');
    label.textContent = title;

    field.append(label, control);

    if (error) this.errorTargets.push({ field, error });

    row.append(field);
    details.append(row);

    return field;
  }

  textField(details, title, caption, value, error) {
    const input = document.createElement('input');
    input.type = 'text';
    input.value = value ?? '';

    this.formField(details, title, caption, input, error);

    return { input };
  }

  numberField(details, title, caption, value, error) {
    const input = document.createElement('input');
    input.type = 'number';
    input.value = value === null || value === undefined ? '' : String(value);

    this.formField(details, title, caption, input, error);

    return { input };
  }

  textareaField(details, title, caption, value, error) {
    const textarea = document.createElement('textarea');
    textarea.value = value ?? '';

    this.formField(details, title, caption, textarea, error);

    return { textarea };
  }

  selectField(details, title, caption, options, value, onChange, typeList, error) {
    const select = document.createElement('rdio-select');
    select.setAttribute('options', JSON.stringify(options));
    select.setAttribute('value', value);
    select.addEventListener('change', (ev) => {
      // The primitive re-dispatches a CustomEvent with the string value; the
      // bubbled native change carries null (trusted) or undefined (synthetic)
      // detail and must be ignored, otherwise it clobbers the CustomEvent.
      if (typeof ev.detail === 'string') onChange(ev.detail);
    });

    this.formField(details, title, caption, select, error, typeList);

    return select;
  }

  toggleField(details, title, caption, checked, onChange) {
    const toggle = document.createElement('rdio-toggle');
    toggle.checked = !!checked;
    toggle.addEventListener('change', (ev) => onChange(ev.detail));

    const row = document.createElement('div');
    row.className = 'row';

    row.append(this.captionBlock(title, caption), toggle);
    details.append(row);

    return toggle;
  }

  deleteRow(details, label, onClick, container) {
    const bottom = container ?? document.createElement('div');
    bottom.className = 'row bottom';

    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'rdio-button-mini delete warn';
    button.textContent = label;
    button.addEventListener('click', onClick);

    bottom.append(button);
    details.append(bottom);
  }

  addSystem() {
    const rows = [newRow('systems'), ...this.systems.map((row, i) => ({ ...row, order: i + 2 }))];

    this.commit(rows, false);
  }
}

customElements.define('rdio-admin-systems', RdioAdminSystems);

function bindSectionSlices(callback) {
  const { store } = getAdminContext();

  return store.subscribe([`section:${NAME}`, 'section:groups', 'section:tags', 'alerts'], callback);
}

function rowKey(row, index) {
  return typeof row.id === 'number' && row.id > 0 ? `id${row.id}` : `i${index}`;
}

function parseNumber(value) {
  return value === '' || value === null ? null : Number(value);
}

function newRow(listKey) {
  const base = { id: 0, order: 0 };

  if (listKey === 'systems') return { ...base, alert: '', autoPopulate: false, blacklists: '', delay: null, label: '', led: '', systemRef: null, sites: [], talkgroups: [], type: '', units: [] };
  if (listKey === 'sites') return { ...base, label: '', siteRef: null };
  if (listKey === 'talkgroups') return { ...base, alert: '', delay: null, frequency: null, groupIds: [], label: '', led: '', name: '', tagId: null, talkgroupRef: null, type: '' };

  return { ...base, label: '', unitFrom: null, unitRef: null, unitTo: null };
}
