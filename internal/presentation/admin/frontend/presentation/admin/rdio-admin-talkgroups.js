// rdio-admin-talkgroups: the talkgroups of one system, saved on
// their own. Same scaffold as the sites and units sections, plus the fields a
// talkgroup carries (groups, tag, alert, delay, frequency, led, type) and the
// Blacklist action, which appends the talkgroup id to the system's blacklists
// and removes the row, like the legacy tool.

import { playAlert } from './play-alert.js';
import { getAdminContext } from '../../application/admin-context.js';
import { talkgroupErrors } from '../../domain/systems-validation.js';
import { sectionWorking, setSectionWorking, setSubWorking } from './section-shell.js';
import { connectSubSection, disconnectSubSection, renderSubSection, rowPanelKey } from './sub-section-shell.js';
import {
  deleteRow,
  numberField,
  parseNumber,
  selectField,
  textField,
  titleCase,
  makePanel,
} from './row-builders.js';

const LEVEL = 'talkgroups';

const CAPTION = 'System talkgroups, you can drag and drop talkgroups to rearrange them. They will appear with the same order on the talkgroup selection panel unless the sort talkgroups option is enabled. Pick the system the talkgroups belong to.';

const SPEC = {
  level: LEVEL,
  caption: CAPTION,
  addLabel: 'New talkgroup',
  rowPrefix: 'tg',
  listen: ['section:groups', 'section:tags', 'alerts'],
  buildRow: talkgroupRow,
};

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

const TALKGROUP_ALERT_CAPTION = 'Prefixes an alert sound to all calls assigned from this talkgroup. The alert assignment order is as follows: Group -> Tag -> System -> Talkgroup.';
const LED_CAPTION = 'The default LED color is green but it can be changed at several levels. The color assignment order is as follows: Group -> Tag -> System -> Talkgroup.';

function freshRows(component) {
  return getAdminContext().store.getState()[`subsection:${LEVEL}:${component.systemId}`]?.working ?? [];
}

function talkgroupRow(component, talkgroup, index, rows) {
  const groups = (sectionWorking('groups') ?? []).filter((group) => typeof group.id === 'number' && group.id > 0);
  const tags = (sectionWorking('tags') ?? []).filter((tag) => typeof tag.id === 'number' && tag.id > 0);
  const alerts = getAdminContext().store.getState().alerts ?? {};

  const errors = talkgroupErrors(talkgroup, rows, {
    groupIds: groups.map((group) => group.id),
    tagIds: tags.map((tag) => tag.id),
  });

  const key = rowPanelKey(component, talkgroup, index);

  const details = makePanel(component.openState, key, talkgroup.label?.trim() || 'NewTalkgroup', Object.keys(errors).length);

  const update = (patch) => {
    setSubWorking(LEVEL, component.systemId, freshRows(component).map((t, i) => (i === index ? { ...t, ...patch } : t)));
  };

  const refField = numberField(component.errorTargets, details, 'Id', 'Talkgroup identifier in decimal format.', talkgroup.talkgroupRef, errors.talkgroupRef);
  refField.input.min = '1';
  refField.input.step = '1';
  refField.input.addEventListener('change', () => update({ talkgroupRef: parseNumber(refField.input.value) }));

  const labelField = textField(component.errorTargets, details, 'Label', 'Talkgroup label displayed on the main screen and on buttons.', talkgroup.label, errors.label);
  labelField.input.addEventListener('change', () => update({ label: labelField.input.value }));

  const nameField = textField(component.errorTargets, details, 'Name', 'Talkgroup name displayed on the main screen.', talkgroup.name, errors.name);
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

  selectField(component.errorTargets, details, 'Tag', 'Tag to which this talkgroups belongs.', tagOptions, talkgroup.tagId === null || talkgroup.tagId === undefined ? '' : String(talkgroup.tagId), (value) => update({ tagId: value === '' ? null : Number(value) }), null, errors.tagId);

  const alertOptions = [['', 'No Alert'], ...Object.keys(alerts).map((alert) => [alert, titleCase(alert)])];

  selectField(component.errorTargets, details, 'Audio alert', TALKGROUP_ALERT_CAPTION, alertOptions, talkgroup.alert ?? '', (value) => {
    update({ alert: value });

    if (value) playAlert(alerts, value);
  });

  const delayField = numberField(component.errorTargets, details, 'Delay', 'Delay in minutes before propagating calls to clients.', talkgroup.delay);
  delayField.input.min = '0';
  delayField.input.step = '1';
  delayField.input.placeholder = 'Minutes';
  delayField.input.addEventListener('change', () => update({ delay: parseNumber(delayField.input.value) }));

  const frequencyField = numberField(component.errorTargets, details, 'Frequency', 'Fake frequency in hertz displayed on the main screen.', talkgroup.frequency, errors.frequency);
  frequencyField.input.min = '0';
  frequencyField.input.addEventListener('change', () => update({ frequency: parseNumber(frequencyField.input.value) }));

  selectField(component.errorTargets, details, 'Led Color', LED_CAPTION, LEDS, talkgroup.led ?? '', (value) => update({ led: value }));

  selectField(component.errorTargets, details, 'Type', 'Type of radio system that will override the type defined at the system level, if any.', TYPES, talkgroup.type ?? '', (value) => update({ type: value }), TYPE_CAPTION);

  const bottom = document.createElement('div');
  bottom.className = 'row bottom';

  if (talkgroup.id) {
    const blacklist = document.createElement('button');
    blacklist.type = 'button';
    blacklist.className = 'rdio-button-mini blacklist';
    blacklist.textContent = 'Blacklist talkgroup';
    blacklist.addEventListener('click', () => {
      // The blacklists live on the system row, which the Systems section saves;
      // the row removal lives in this level's slice.
      const systems = sectionWorking('systems') ?? [];

      setSectionWorking('systems', systems.map((system) => {
        if (system.id !== component.systemId) return system;

        const blacklists = system.blacklists?.trim() ? `${system.blacklists},${talkgroup.id}` : `${talkgroup.id}`;

        return { ...system, blacklists };
      }));

      setSubWorking(LEVEL, component.systemId, freshRows(component).filter((_, i) => i !== index));
    });

    bottom.append(blacklist);
  }

  deleteRow(details, 'Delete talkgroup', () => {
    setSubWorking(LEVEL, component.systemId, freshRows(component).filter((_, i) => i !== index));
  }, bottom);

  return details;
}

class RdioAdminTalkgroups extends HTMLElement {
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

customElements.define('rdio-admin-talkgroups', RdioAdminTalkgroups);
