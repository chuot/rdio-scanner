// rdio-admin-talkgroups: the talkgroups of one system, saved on their
// own: same scaffold as the sites and units sections, plus the
// Blacklist action that removes the row and stores the blacklist.

import { playAlert } from './play-alert.js';
import { getAdminContext } from '../../application/admin-context.js';
import { subSectionInvalid, talkgroupErrors } from '../../domain/systems-validation.js';
import { showSnackbar } from '../../../../common/frontend/presentation/ui/primitives.js';
import { sectionWorking } from './section-shell.js';
import { commitSubRows, connectSubSection, disconnectSubSection, refreshField, renderSubSection, rowPanelKey } from './sub-section-shell.js';
import {
  deleteRow,
  numberField,
  onFieldEdit,
  parseNumber,
  rowContext,
  selectField,
  setRowMarker,
  markRequired,
  textField,
  titleCase,
  makePanel,
} from './row-builders.js';

const LEVEL = 'talkgroups';

const CAPTION = 'System talkgroups, you can drag and drop talkgroups to rearrange them. They will appear with the same order on the talkgroup selection panel unless the sort talkgroups option is enabled. Pick the system the talkgroups belong to. Blacklisting a talkgroup removes its row and stores the system\'s blacklist entry with the rows, in the click.';

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
  ['edacs', 'EDACS'],
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
  ['EDACS', 'Enhanced digital access communications system.'],
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

// unstoredTalkgroupFields names the fields the section refuses to
// save, so the Blacklist refusal says which rows to finish first.
function unstoredTalkgroupFields(rows, groups, tags) {
  const knownIds = { groupIds: groups.map((group) => group.id), tagIds: tags.map((tag) => tag.id) };

  const messages = [];

  for (const row of rows) {
    for (const message of Object.values(talkgroupErrors(row, rows, knownIds))) {
      if (!messages.includes(message)) messages.push(message);
    }
  }

  return messages.join(', ');
}

// systemsBlacklistDraft reads the Systems section's draft of the
// Blacklists field and the text behind it: the Blacklist action is
// about to replace a differing draft the operator must hear about.
function systemsBlacklistDraft(systemId) {
  const systems = getAdminContext().store.getState()['section:systems'] ?? {};

  const draft = (systems.working ?? []).find((system) => system.id === systemId)?.blacklists ?? '';
  const saved = (systems.baseline ?? []).find((system) => system.id === systemId)?.blacklists ?? '';

  return { draft, saved, replaced: draft !== saved };
}

function talkgroupRow(component, talkgroup, index, rows) {
  const groups = (sectionWorking('groups') ?? []).filter((group) => typeof group.id === 'number' && group.id > 0);
  const tags = (sectionWorking('tags') ?? []).filter((tag) => typeof tag.id === 'number' && tag.id > 0);
  const alerts = getAdminContext().store.getState().alerts ?? {};

  const knownIds = { groupIds: groups.map((group) => group.id), tagIds: tags.map((tag) => tag.id) };

  // errorsFor recomputes talkgroupErrors per read: duplicate identifier
  // is a whole-list rule, so the colliding row must be in the current
  // list and knownIds.
  const errorsFor = (list) => talkgroupErrors(list[index], list, knownIds);

  const errors = errorsFor(rows);

  const key = rowPanelKey(component, talkgroup, index);
  const ctx = rowContext(component, key);

  const details = makePanel(component.openState, key, talkgroup.label?.trim() || 'NewTalkgroup', Object.keys(errors).length,
    { index, total: rows.length });

  const update = (patch, field, name) => {
    const next = freshRows(component).map((t, i) => (i === index ? { ...t, ...patch } : t));

    commitSubRows(component, next, true);

    const nextErrors = errorsFor(next);

    if (field) refreshField(component, field, nextErrors[name] ?? '');

    setRowMarker(details, Object.keys(nextErrors).length > 0);
  };

  const refField = numberField(ctx, details, 'talkgroupRef', 'Id', 'Talkgroup identifier in decimal format.', talkgroup.talkgroupRef, errors.talkgroupRef, true);
  refField.input.min = '1';
  refField.input.step = '1';
  onFieldEdit(refField.input, () => update({ talkgroupRef: parseNumber(refField.input.value) }, refField.field, 'talkgroupRef'));

  const labelField = textField(ctx, details, 'label', 'Label', 'Talkgroup label displayed on the main screen and on buttons.', talkgroup.label, errors.label, true);
  onFieldEdit(labelField.input, () => update({ label: labelField.input.value }, labelField.field, 'label'));

  const nameField = textField(ctx, details, 'name', 'Name', 'Talkgroup name displayed on the main screen.', talkgroup.name, errors.name, true);
  onFieldEdit(nameField.input, () => update({ name: nameField.input.value }, nameField.field, 'name'));

  // groupIds — checkbox list of the saved groups (multi-select). The list is a field of its own:
  // the row under it is marked only once the operator has reached the list, the same rule the
  // fields inside a form field follow.
  const groupsKey = `${ctx.key}:groupIds`;

  const groupsRow = document.createElement('div');
  groupsRow.className = 'row';

  const groupsHelp = document.createElement('p');
  const groupsTitle = document.createElement('span');
  groupsTitle.className = 'body';
  groupsTitle.textContent = 'Groups';

  const groupsCaption = document.createElement('span');
  groupsCaption.className = 'caption';
  groupsCaption.textContent = 'Groups to which this talkgroups belongs.';

  groupsHelp.append(groupsTitle, document.createElement('br'), groupsCaption);

  // required on every row, marker on the caption naming the list: built
  // outside a form field, so the declaration rides the pair the
  // stylesheet reads.
  markRequired(groupsRow, groupsTitle);

  const groupsList = document.createElement('div');
  groupsList.className = 'rdio-checkbox-list';

  const readGroupIds = (row) => (Array.isArray(row?.groupIds) ? row.groupIds : typeof row?.groupIds === 'number' ? [row.groupIds] : []);

  const selectedGroupIds = readGroupIds(talkgroup);

  const writeGroupsMarker = (rows) => {
    const invalid = component.reached.has(groupsKey) && Boolean(errorsFor(rows).groupIds);

    if (invalid) groupsRow.dataset.invalid = 'true';
    else delete groupsRow.dataset.invalid;
  };

  for (const group of groups) {
    const label = document.createElement('label');

    const checkbox = document.createElement('input');
    checkbox.type = 'checkbox';
    checkbox.checked = selectedGroupIds.includes(group.id);
    checkbox.addEventListener('change', () => {
      // readGroupIds takes the selection the store holds right now: the
      // edited row stays on the page, so a pick adds to what it holds.
      const selected = readGroupIds(freshRows(component)[index]);

      const next = checkbox.checked ? [...selected, group.id] : selected.filter((id) => id !== group.id);

      component.reached.add(groupsKey);

      update({ groupIds: next });

      writeGroupsMarker(freshRows(component));
    });

    const text = document.createElement('span');
    text.textContent = group.label;

    label.append(checkbox, text);
    groupsList.append(label);
  }

  writeGroupsMarker(rows);

  groupsRow.append(groupsHelp, groupsList);
  details.append(groupsRow);

  const tagOptions = tags.map((tag) => [String(tag.id), tag.label]);

  const tagField = selectField(ctx, details, 'tagId', 'Tag', 'Tag to which this talkgroups belongs.', tagOptions, talkgroup.tagId === null || talkgroup.tagId === undefined ? '' : String(talkgroup.tagId), (value) => update({ tagId: value === '' ? null : Number(value) }, tagField.field, 'tagId'), null, errors.tagId, true);

  const alertOptions = [['', 'No Alert'], ...Object.keys(alerts).map((alert) => [alert, titleCase(alert)])];

  selectField(ctx, details, 'alert', 'Audio alert', TALKGROUP_ALERT_CAPTION, alertOptions, talkgroup.alert ?? '', (value) => {
    update({ alert: value });

    if (value) playAlert(alerts, value);
  });

  const delayField = numberField(ctx, details, 'delay', 'Delay', 'Delay in minutes before propagating calls to clients.', talkgroup.delay);
  delayField.input.min = '0';
  delayField.input.step = '1';
  delayField.input.placeholder = 'Minutes';
  delayField.input.addEventListener('change', () => update({ delay: parseNumber(delayField.input.value) }, delayField.field, 'delay'));

  const frequencyField = numberField(ctx, details, 'frequency', 'Frequency', 'Fake frequency in hertz displayed on the main screen.', talkgroup.frequency, errors.frequency);
  frequencyField.input.min = '0';
  onFieldEdit(frequencyField.input, () => update({ frequency: parseNumber(frequencyField.input.value) }, frequencyField.field, 'frequency'));

  selectField(ctx, details, 'led', 'Led Color', LED_CAPTION, LEDS, talkgroup.led ?? '', (value) => update({ led: value }));

  selectField(ctx, details, 'type', 'Type', 'Type of radio system that will override the type defined at the system level, if any.', TYPES, talkgroup.type ?? '', (value) => update({ type: value }), TYPE_CAPTION);

  const bottom = document.createElement('div');
  bottom.className = 'row bottom';

  if (talkgroup.id) {
    const blacklist = document.createElement('button');
    blacklist.type = 'button';
    blacklist.className = 'rdio-button-mini blacklist';
    blacklist.textContent = 'Blacklist talkgroup';
    blacklist.addEventListener('click', async () => {
      // the Blacklist click stores both halves at once: the row goes away
      // and the entry names the talkgroup's reference.
      const rows = freshRows(component).filter((_, i) => i !== index);
      const groups = sectionWorking('groups') ?? [];
      const tags = sectionWorking('tags') ?? [];

      if (subSectionInvalid(LEVEL, rows, groups, tags)) {
        showSnackbar(`Blacklisting needs the talkgroups saved first: ${unstoredTalkgroupFields(rows, groups, tags)}`);

        return;
      }

      commitSubRows(component, rows);

      // Taken before anything is written: the Systems section's own draft of this system's list, which
      // the list about to be stored replaces.
      const draft = systemsBlacklistDraft(component.systemId);

      getAdminContext().useCases.blacklistTalkgroup(component.systemId, talkgroup.talkgroupRef);

      if (!await getAdminContext().useCases.saveSubSection(LEVEL, component.systemId)) {
        showSnackbar(`Unable to save the talkgroups of system ${component.systemRef ?? component.systemId}`);

        return;
      }

      if (draft.replaced) {
        showSnackbar('The Blacklists field of the Systems section held a list you had not saved. It now holds the list the server stored.');
      }
    });

    bottom.append(blacklist);
  }

  deleteRow(details, 'Delete talkgroup', () => {
    commitSubRows(component, freshRows(component).filter((_, i) => i !== index));
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
