// rdio-admin-dirwatch: the dirwatch section: collapsible row panels,
// one open at a time: toggles, selects, mask, frequency, delay, drag sort

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
import { attachSingleOpenRule, captionBlock, makePanel, markField, markRequired, markRowIdentity, onFieldEdit, rowKey, setRowMarker, setRowMoveLabels } from './row-builders.js';
import { clearReached, harvestReached, restoreReached } from './sub-section-shell.js';
import { DIRWATCH_MASK_TOKENS, validateDirwatchRow } from '../../domain/dirwatch.js';

const NAME = 'dirwatch';

// dirwatch panels carry the 'dw' prefix: the open row follows the remembered row
const PANEL_PREFIX = 'dw';

const TYPE_OPTIONS = [
  ['default', 'Default'],
  ['dsdplus', 'DSDPlus Fast Lane'],
  ['sdr-trunk', 'SDR Trunk'],
  ['trunk-recorder', 'Trunk Recorder'],
];

const TYPE_HELP = [
  ['Default', 'Extract the metadata from a custom mask.'],
  ['DSDPlus Fast Lane', 'Extract the metadata from the file path.'],
  ['SDR Trunk', "Extract the metadata from the MP3 tags defined on the SDR Trunk's aliases tab."],
  ['Trunk Recorder', 'Extract the metadata from the json file.'],
];

const DIRECTORY_BOLD = {
  dsdplus: ['Record', '1R-Record', 'VC-Record'],
  'sdr-trunk': ['Recordings'],
  'trunk-recorder': ['captureDir'],
};

const MASK_TOKEN_HELP = {
  '#DATE': 'extract the date like 20201231 (YYYYMMMDD), 2020-12-31 (YYYY-MM-DD) or 2020_12_31 (YYYY_MM_DD).',
  '#GROUP': 'extract the group label.',
  '#HZ': 'extract the frequency in hertz like 119100000.',
  '#KHZ': 'extract the frequency in kilohertz like 119100.',
  '#MHZ': 'extract the frequency in megahertz like 119.100.',
  '#SITE': 'extract the site id like 1.',
  '#SITELBL': 'extract the site label.',
  '#SYS': 'extract the system id like 11.',
  '#SYSLBL': 'extract the system label.',
  '#TAG': 'extract the tag label.',
  '#TG': 'extract the talkgroup id like 1457 in decimal format.',
  '#TGAFS': 'extract the talkgroup id like 11-061 in AFS (agency-fleet-subfleet) format.',
  '#TGHZ': 'extract the frequency in hertz like 119100000 and set the talkgroup id to 119100.',
  '#TGKHZ': 'extract the frequency in kilohertz like 119100.000 or 119100 and set the talkgroup id to 119100.',
  '#TGLBL': 'extract the talkgroup label.',
  '#TGMHZ': 'extract the frequency in megahertz like 119.100000 or 119 and set the talkgroup id to 119100.',
  '#TIME': 'extract the local time like 0853439 (HHMMSS), 08-34-39 (HH-MM-SS) or 08:34:39 (HH:MM:SS).',
  '#UNIT': 'extract the unit id like 4424001.',
  '#UNITLBL': 'extract the unit label. Also requires #UNIT.',
  '#ZTIME': 'extract the zulu time like 0453439 (HHMMSS), 08-34-39 (HH-MM-SS) or 04:34:39 (HH:MM:SS).',
};

class RdioAdminDirwatch extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    this.unsubscribes = [
      bindSectionSlice(NAME, () => this.render()),
      bindSectionSlice('systems', () => this.render()),
    ];

    // reached fields are remembered under keys naming the dirwatch slice
    this.reached = new Set();
    this.rowKeyPrefix = `section:${NAME}`;

    // openState: which dirwatch rows stay open across store rebuilds
    this.openState = new Map();

    this.render();
  }

  disconnectedCallback() {
    for (const unsubscribe of this.unsubscribes ?? []) unsubscribe();
  }

  get rows() {
    return sortedByOrder(sectionWorking(NAME));
  }

  render(force = false) {
    const slice = sectionSlice(NAME);
    const systemsSlice = sectionSlice('systems');

    if (!slice) return;

    if (!force && slice.working === this.internalEdit && systemsSlice?.working === this.systemsRef) return;

    // open rows are read off the dirwatch page before rows are built again,
    // and reproduced from the section's openState map
    for (const details of this.querySelectorAll('details[data-key]')) {
      this.openState.set(details.dataset.key, details.open);
    }

    harvestReached(this);

    // a clean dirwatch slice starts with every field unreached
    if (!slice.dirty) clearReached(this, this.rowKeyPrefix);

    const rows = this.rows;
    const errors = rows.map((row) => validateDirwatchRow(row, rows));
    const invalid = errors.some((rowErrors) => Object.keys(rowErrors).length > 0);

    if (slice.invalid !== invalid) setSectionInvalid(NAME, invalid);

    const root = document.createElement('div');
    root.className = 'rdio-admin-card dirwatch';

    const top = document.createElement('div');
    top.className = 'row top';

    const intro = document.createElement('p');
    intro.textContent = rows.length
      ? 'Define a dirwatch to monitor new audio files from any directory.'
      : 'No dirwatch defined.';

    const add = document.createElement('button');
    add.type = 'button';
    add.className = 'rdio-button-mini add';
    add.textContent = 'New dir watch';
    add.addEventListener('click', () => this.add());

    top.append(intro, add);
    root.append(top);

    const list = document.createElement('div');
    list.className = 'rdio-admin-rows';

    this.views = [];

    rows.forEach((row, index) => {
      const { panel, view } = this.rowElement(row, index, rows, errors[index]);

      this.views[index] = view;
      list.append(panel);
    });

    attachDragSort(list, (from, to) => this.commitRows(reorderAfterDrag(this.rows, from, to)));

    // one dirwatch row open at a time keeps the list short
    // and the save bar reachable without scrolling
    attachSingleOpenRule(list, this.openState);

    root.append(list);

    const bar = document.createElement('rdio-admin-section-bar');
    bar.dataset.section = NAME;
    root.append(bar);

    this.systemsRef = systemsSlice?.working;

    this.replaceChildren(root);

    // rdio-form-field only exposes setError once it is connected.
    for (const view of this.views) {
      for (const [key, field] of view.fields) {
        if (field.setError) field.setError(view.errors[key] ?? '');
      }
    }

    restoreReached(this);
  }

  rowElement(row, index, rows, errors) {
    const type = row.type ?? 'default';
    const view = { errors, fields: new Map() };

    // the dirwatch panel key from PANEL_PREFIX and rowKey follows the row itself
    const key = `${PANEL_PREFIX}:${rowKey(row, index)}`;

    const panel = makePanel(this.openState, key, row.directory || 'New dirwatch', Object.keys(errors).length, { index, total: rows.length });

    view.title = panel.querySelector('.row-title');
    view.panel = panel;

    panel.append(this.toggleRow('Disabled', 'Disable the dirwatch.', !!row.disabled, (value) => {
      this.updateRow(index, { disabled: value });
    }));

    panel.append(this.toggleRow(
      'Delete After',
      'Delete the audio file after being ingested. If activated, all pre-existing audio files will be ingested and deleted as soon as the server starts. If not activated, pre-existing audio files are neither ingested nor deleted.',
      !!row.deleteAfter,
      (value) => this.updateRow(index, { deleteAfter: value }),
    ));

    panel.append(this.typeRow(row, index));

    const directoryField = this.field('Directory', true);
    const directoryInput = directoryField.querySelector('input');
    directoryInput.value = row.directory ?? '';
    directoryInput.required = true;
    onFieldEdit(directoryInput, () => this.updateRow(index, { directory: directoryInput.value }));
    view.fields.set('directory', directoryField);

    panel.append(this.fieldRow('Directory', this.directoryCaption(type), directoryField));

    if (['default', 'dsdplus', 'trunk-recorder'].includes(type)) {
      const extensionField = this.field('Extension');
      const extensionInput = extensionField.querySelector('input');
      extensionInput.value = row.extension ?? '';
      onFieldEdit(extensionInput, () => this.updateRow(index, { extension: extensionInput.value }));
      view.fields.set('extension', extensionField);

      panel.append(this.fieldRow('Extension', 'The audio call extension to monitor without the period. Ex.: "mp3", "wav".', extensionField));
    }

    if (['default', 'dsdplus'].includes(type)) {
      const systems = (sectionWorking('systems') ?? []).filter((system) => Number.isFinite(system.id) && system.id > 0);
      const system = systems.find((candidate) => candidate.id === row.systemId);

      const systemField = this.selectField('System', [['', ''], ...systems.map((system) => [String(system.id), system.label])], row.systemId, (value) => {
        this.updateRow(index, { systemId: value === '' ? null : Number(value) });
      });
      view.fields.set('systemId', systemField);

      panel.append(this.fieldRow('System', 'System to where the audio files should go.', systemField));

      const sites = (system?.sites ?? []).filter((site) => Number.isFinite(site.id) && site.id > 0);
      const siteField = this.selectField('Site', [['', ''], ...sites.map((site) => [String(site.id), site.label])], row.siteId, (value) => {
        this.updateRow(index, { siteId: value === '' ? null : Number(value) });
      });
      view.fields.set('siteId', siteField);

      panel.append(this.fieldRow('Site', 'Site to where the audio files should go.', siteField));

      const talkgroups = (system?.talkgroups ?? []).filter((talkgroup) => Number.isFinite(talkgroup.id) && talkgroup.id > 0);
      const talkgroupField = this.selectField('Talkgroup', [['', ''], ...talkgroups.map((talkgroup) => [String(talkgroup.id), talkgroup.label])], row.talkgroupId, (value) => {
        this.updateRow(index, { talkgroupId: value === '' ? null : Number(value) });
      });
      view.fields.set('talkgroupId', talkgroupField);

      panel.append(this.fieldRow('Talkgroup', 'Talkgroup to where the audio files should go.', talkgroupField));
    }

    if (type === 'default') {
      const maskField = this.field('Mask');
      const maskInput = maskField.querySelector('input');
      maskInput.value = row.mask ?? '';
      onFieldEdit(maskInput, () => this.updateRow(index, { mask: maskInput.value }));
      view.fields.set('mask', maskField);

      panel.append(this.fieldRow('Mask', this.maskCaption(), maskField));

      const frequencyField = this.field('Frequency');
      const frequencyInput = frequencyField.querySelector('input');
      frequencyInput.type = 'number';
      frequencyInput.min = '1';
      frequencyInput.value = row.frequency ?? '';
      onFieldEdit(frequencyInput, () => {
        this.updateRow(index, { frequency: frequencyInput.value === '' ? undefined : Number(frequencyInput.value) });
      });
      view.fields.set('frequency', frequencyField);

      panel.append(this.fieldRow('Frequency', 'Fake frequency in hertz displayed on the main screen.', frequencyField));

      const delayField = this.field('Delay');
      const delayInput = delayField.querySelector('input');
      delayInput.type = 'number';
      delayInput.min = '2000';
      delayInput.value = row.delay ?? '';
      onFieldEdit(delayInput, () => {
        this.updateRow(index, { delay: delayInput.value === '' ? undefined : Number(delayInput.value) });
      });
      view.fields.set('delay', delayField);

      panel.append(this.fieldRow('Delay', 'Depending on the recorder, audio files can be ingested too soon after the recorder has created the file. You can set a delay value in milliseconds for the audio file to settle before ingesting it.', delayField));
    }

    // markField keys each dirwatch field with rowKey(row, index) plus the field name,
    // so a rebuild hands the message back to the field it belongs to
    for (const [key, field] of view.fields) markField(this, field, rowKey(row, index), key);

    const bottom = document.createElement('div');
    bottom.className = 'row bottom';

    const remove = document.createElement('button');
    remove.type = 'button';
    remove.className = 'rdio-button-mini delete warn';
    remove.textContent = 'Delete dirwatch';
    remove.addEventListener('click', () => {
      this.commitRows(this.rows.filter((_, i) => i !== index));
    });

    bottom.append(remove);
    panel.append(bottom);

    return { panel, view };
  }

  typeRow(row, index) {
    const r = document.createElement('div');
    r.className = 'row';

    // The standard caption block carries the type list, the same builder the
    // systems section uses for its Type caption.
    const info = captionBlock('Type', 'Dirwatch type defines how the metadata are obtained.', TYPE_HELP);

    const field = this.selectField('Type', TYPE_OPTIONS, row.type ?? 'default', (value) => {
      this.updateRow(index, { type: value });
    });

    r.append(info, field);

    return r;
  }

  fieldRow(title, caption, control) {
    const r = document.createElement('div');
    r.className = 'row';

    r.append(captionBlock(title, caption), control);

    return r;
  }

  toggleRow(title, caption, checked, onChange) {
    const r = document.createElement('div');
    r.className = 'row';

    const toggle = document.createElement('rdio-toggle');
    toggle.checked = checked;
    toggle.setAttribute('aria-label', title);
    toggle.addEventListener('change', (ev) => onChange(ev.detail));

    r.append(captionBlock(title, caption), toggle);

    return r;
  }

  directoryCaption(type) {
    const parts = [document.createTextNode('Path of the ')];

    const bold = DIRECTORY_BOLD[type];

    if (bold) {
      bold.forEach((word, i) => {
        if (i) parts.push(document.createTextNode(i === bold.length - 1 ? ' or ' : ', '));

        const b = document.createElement('b');
        b.textContent = word;
        parts.push(b);
      });
    } else {
      parts.push(document.createTextNode('local'));
    }

    parts.push(document.createTextNode(' directory to monitor for file ingestion. Note that dirwatch is not compatible with networked disk.'));

    return parts;
  }

  maskCaption() {
    const parts = [document.createTextNode('Some metadata can be extracted from the file name of the audio file using specific META tags. Possible META tags:')];

    const list = document.createElement('ul');

    for (const token of DIRWATCH_MASK_TOKENS) {
      const item = document.createElement('li');
      const bold = document.createElement('b');
      bold.textContent = token;
      item.append(bold, ` - ${MASK_TOKEN_HELP[token]}`);
      list.append(item);
    }

    parts.push(list, document.createTextNode('Example: cymx_#TG_#DATE_#TIME_#HZ'));

    return parts;
  }

  field(label, required) {
    const field = document.createElement('rdio-form-field');

    const labelEl = document.createElement('label');
    labelEl.textContent = label;

    const input = document.createElement('input');
    input.type = 'text';

    // the asterisk mark declares the dirwatch field required on every row,
    // split between the label and the field host so the control announces it
    // a dirwatch field required only on some rows stays plain.
    if (required) markRequired(field, labelEl);

    field.append(labelEl, input);

    return field;
  }

  selectField(label, options, value, onChange) {
    const field = document.createElement('rdio-form-field');

    const labelEl = document.createElement('label');
    labelEl.textContent = label;

    const select = document.createElement('rdio-select');
    select.setAttribute('options', JSON.stringify(options));
    select.setAttribute('value', String(value ?? ''));
    select.addEventListener('change', (ev) => {
      // dirwatch: rdio-select re-dispatches the inner select's value as a CustomEvent
      // the native change event bubbles here too but carries no detail
      if (typeof ev.detail === 'string') onChange(ev.detail);
    });

    field.append(labelEl, select);

    return field;
  }

  updateRow(index, patch) {
    const rows = this.rows.map((row, i) => (i === index ? { ...row, ...patch } : row));

    this.commitRows(rows, true);

    // Type and systemId drive field visibility / options: rebuild the section.
    if ('type' in patch || 'systemId' in patch) {
      this.render(true);

      return;
    }

    // no full re-render here: focus is preserved, so refresh the edited dirwatch row's
    // title, icon and field errors live
    const view = this.views?.[index];

    if (!view) return;

    const errors = validateDirwatchRow(rows[index], rows);

    view.errors = errors;
    view.title.textContent = rows[index].directory || 'New dirwatch';
    setRowMoveLabels(view.panel, view.title.textContent);
    setRowMarker(view.panel, Object.keys(errors).length > 0);

    for (const [key, field] of view.fields) field.setError(errors[key] ?? '');
  }

  // every dirwatch commit carries section validity to the Save control:
  // add, delete and move change the rows it checks
  // a move reads this.rows at drop time to keep typed values
  commitRows(rows, selfEdit = false) {
    if (selfEdit) this.internalEdit = rows;

    setSectionWorking(NAME, rows);
    setSectionInvalid(NAME, rows.some((row) => Object.keys(validateDirwatchRow(row, rows)).length > 0));
  }

  add() {
    // new dirwatch rows start with mask '#SYS_#TG' and frequency 1: the server
    // answers an unset mask or frequency with a value its rule rejects: an unset
    // row would validate as new but not as saved.
    const fresh = markRowIdentity({ delay: 2000, deleteAfter: true, directory: '', disabled: false, frequency: 1, mask: '#SYS_#TG', order: 0, type: 'default' });
    const rows = [fresh, ...this.rows.map((row, i) => ({ ...row, order: i + 2 }))];

    // the added dirwatch row opens for filling in as the section's one open row;
    // rows left open on the page close too: the next render reads them off the page
    for (const row of this.querySelectorAll('details[data-key]')) row.open = false;

    this.openState.clear();
    this.openState.set(`${PANEL_PREFIX}:${rowKey(fresh, 0)}`, true);

    this.commitRows(rows);
  }
}

customElements.define('rdio-admin-dirwatch', RdioAdminDirwatch);
