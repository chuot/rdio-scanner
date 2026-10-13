// The two things the field-message fix must leave alone: the warning icon a row carries, and the
// Save control of the section holding it. Both answer the row's validity, not the operator's contact
// with it — Angular Material showed the panel icon for an invalid row nobody had touched, and the
// save bar is disabled while the section is invalid. These are the proofs that pin them, taken in
// every section that marks its rows.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-systems.js';
import '../presentation/admin/rdio-admin-sites.js';
import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-units.js';
import '../presentation/admin/rdio-admin-groups.js';
import '../presentation/admin/rdio-admin-tags.js';
import '../presentation/admin/rdio-admin-access.js';
import '../presentation/admin/rdio-admin-apikeys.js';
import '../presentation/admin/rdio-admin-downstreams.js';
import '../presentation/admin/rdio-admin-dirwatch.js';

import { connectFields, findAll, texts } from './dom-shim.js';

const METRO = { id: 1, label: 'Metro', order: 2, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };
const COUNTY = { id: 2, label: 'County', order: 1, systemRef: 2, blacklists: '', sites: [], talkgroups: [], units: [] };

const SITES = {
  1: [],
  2: [{ id: 4, label: 'East', siteRef: 40, order: 1 }],
};

const TALKGROUPS = {
  1: [],
  2: [{ id: 7, label: 'Fire', name: 'Fire dispatch', talkgroupRef: 70, order: 1, groupIds: [1], tagId: 3, alert: '', delay: null, frequency: null, led: '', type: '' }],
};

const UNITS = {
  1: [],
  2: [{ id: 9, label: 'Car', unitRef: 100, unitFrom: 100, unitTo: 199, order: 1 }],
};

const GROUPS = [{ id: 1, label: 'Dispatch', alert: '', led: '' }];
const TAGS = [{ id: 3, label: 'Fire', alert: '', led: '' }];

const ACCESS = [{ id: 1, order: 1, code: 'alpha', ident: 'AAA', systems: '*' }];
const APIKEYS = [{ id: 1, order: 1, key: 'first-key', ident: 'K1', systems: '*', disabled: false }];
const DOWNSTREAMS = [{ id: 1, order: 1, url: 'https://first.example', apikey: 'dk-1', systems: '*', disabled: false }];
const DIRWATCH = [{ id: 1, order: 1, directory: '/var/spool/first', extension: 'mp3', mask: '#SYS #TG', type: 'p25', systemId: 2, siteId: null, talkgroupId: 7, frequency: 1, delay: 2000, disabled: false }];

function slice(working) {
  return { baseline: structuredClone(working), working: structuredClone(working), dirty: false, invalid: false, conflict: false };
}

function seed(store) {
  const state = { auth: { authenticated: true, docker: false, passwordNeedChange: false }, alerts: { fire: {} } };

  for (const [level, bySystem] of [['sites', SITES], ['talkgroups', TALKGROUPS], ['units', UNITS]]) {
    for (const system of [COUNTY, METRO]) {
      state[`subsection:${level}:${system.id}`] = slice(bySystem[system.id]);
    }
  }

  store.setState({
    ...state,
    'section:systems': slice([METRO, COUNTY]),
    'section:groups': slice(GROUPS),
    'section:tags': slice(TAGS),
    'section:access': slice(ACCESS),
    'section:apikeys': slice(APIKEYS),
    'section:downstreams': slice(DOWNSTREAMS),
    'section:dirwatch': slice(DIRWATCH),
  });

  return store;
}

function attach(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

function control(el, className) {
  return findAll(el, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes(className))[0];
}

// The rows on the page: the systems tree sections open a row as a <details>, the row sections build
// a panel. Both are the row the operator sees.
function rows(el) {
  return findAll(el, (node) => node.tag === 'details' || node.className?.split(/\s+/).includes('rdio-admin-row'));
}

function addRow(el) {
  control(el, 'add').click();
  connectFields(el);

  return rows(el)[0];
}

function field(row, title) {
  const fields = findAll(row, (node) => node.tag === 'rdio-form-field');

  // The systems-tree sections and the row sections with a dialog put a <label> inside the field;
  // Groups and Tags name the field in the description row beside it.
  return fields.find((node) => texts(findAll(node, (child) => child.tag === 'label'))[0] === title)
    ?? fields.find((node) => findAll(node.parent, (child) => child.className?.split(/\s+/).includes('field-title'))[0]?.textContent === title);
}

function controlOf(row, title) {
  return findAll(field(row, title), (node) => ['input', 'textarea', 'rdio-select'].includes(node.tag))[0];
}

function typeIn(row, title, value) {
  const control = controlOf(row, title);

  control.value = value;
  control.dispatchEvent({ type: 'change' });
  connectFields(row);

  return control;
}

function pickIn(row, title, value) {
  findAll(row, (node) => node.tag === 'rdio-select')
    .find((node) => texts(findAll(node.parent, (child) => child.tag === 'label'))[0] === title)
    ?.dispatchEvent(new CustomEvent('change', { detail: value }));

  connectFields(row);
}

function checkBox(row) {
  const box = findAll(row, (node) => node.tag === 'input' && node.type === 'checkbox')[0];

  box.checked = true;
  box.dispatchEvent({ type: 'change' });
  connectFields(row);
}

function marker(row) {
  // The dirwatch row keeps its icon on the page and hides it when the row is valid, so the reading
  // is the icon the row is showing, not the node sitting behind it.
  return findAll(row, (node) => node.className?.split(/\s+/).includes('row-invalid') && node.hidden !== true)[0] ?? null;
}

function saveButton(el) {
  const bar = findAll(el, (node) => node.tag === 'rdio-admin-section-bar')[0];

  bar.connectedCallback();

  return findAll(bar, (node) => node.tag === 'button' && node.className.split(/\s+/).includes('save'))[0];
}

function rowButton(row, label) {
  return findAll(row, (node) => node.tag === 'button' && node.textContent === label)[0];
}

// The rows the operator adds, and what each one needs to become valid. Filling a row is typing in
// it, which reaches every field of it: the marker and the Save control are read on rows the
// operator has both touched and left.
const PINNED = [
  ['Systems', 'rdio-admin-systems', (row) => { typeIn(row, 'Label', 'County West'); typeIn(row, 'Id', '3'); }],
  ['Sites', 'rdio-admin-sites', (row) => { typeIn(row, 'Label', 'Central'); typeIn(row, 'Id', '41'); }],
  ['Units', 'rdio-admin-units', (row) => { typeIn(row, 'Label', 'Car 12'); typeIn(row, 'Id', '120'); }],
  ['Talkgroups', 'rdio-admin-talkgroups', (row) => {
    typeIn(row, 'Id', '71');
    typeIn(row, 'Label', 'Car 21');
    typeIn(row, 'Name', 'Car 21 dispatch');
    checkBox(row);
    pickIn(row, 'Tag', '3');
  }],
  ['Groups', 'rdio-admin-groups', (row) => typeIn(row, 'Label', 'Tac')],
  ['Tags', 'rdio-admin-tags', (row) => typeIn(row, 'Label', 'Law')],
  ['Access', 'rdio-admin-access', (row) => { typeIn(row, 'Code', 'scramble'); typeIn(row, 'Ident', 'SCR'); }],
  ['API keys', 'rdio-admin-apikeys', (row) => typeIn(row, 'Ident', 'K2')],
  ['Downstreams', 'rdio-admin-downstreams', (row) => { typeIn(row, 'URL', 'https://second.example'); typeIn(row, 'API key', 'dk-2'); }],
  ['Dirwatch', 'rdio-admin-dirwatch', (row) => typeIn(row, 'Directory', '/var/spool/second')],
];

for (const [name, tag, fillRow] of PINNED) {
  test(`a new ${name.toLowerCase()} row carries its warning icon before the operator reaches anything`, () => {
    seed(getAdminContext().store);

    const row = addRow(attach(tag));

    assert.ok(marker(row), `the row the operator has not touched is marked as the invalid row it is`);
  });

  test(`the ${name.toLowerCase()} Save control stays disabled while the new row is invalid`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);

    addRow(el);

    assert.equal(saveButton(el).disabled, true, 'an invalid row keeps the section unsavable, reached or not');
  });

  test(`the ${name.toLowerCase()} row loses its warning icon, and the section its Save control, when the row carries valid values`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);
    const row = addRow(el);

    assert.ok(marker(row));
    assert.equal(saveButton(el).disabled, true);

    fillRow(row);

    assert.equal(marker(row), null, 'the warning icon follows the row\'s validity, not the fields the operator left');
    assert.equal(saveButton(el).disabled, false, 'a section clean of errors can be saved');
  });
}

// The delete is a commit of the section's rows, and carries the section's validity with it: the Save
// control and the row icons answer the rows the section holds after the delete. The operator fills in
// one new row and leaves another untouched, then deletes the row they filled in.
const DELETES = [
  ['Groups', 'rdio-admin-groups', (row) => typeIn(row, 'Label', 'Tac'), 'Delete Group'],
  ['Tags', 'rdio-admin-tags', (row) => typeIn(row, 'Label', 'Law'), 'Delete Tag'],
  ['Dirwatch', 'rdio-admin-dirwatch', (row) => typeIn(row, 'Directory', '/var/spool/second'), 'Delete dirwatch'],
];

for (const [name, tag, fillRow, deleteLabel] of DELETES) {
  test(`the ${name.toLowerCase()} Save control stays disabled when the delete leaves the row nobody touched`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);

    addRow(el);
    fillRow(rows(el)[0]);

    addRow(el);

    // The row the operator filled in is the second on the page: the second add put a new row above it.
    rowButton(rows(el)[1], deleteLabel).click();
    connectFields(el);

    assert.equal(saveButton(el).disabled, true, 'the section keeps the invalid row nobody touched, and stays unsavable');
    assert.ok(marker(rows(el)[0]), 'the row nobody touched keeps its warning icon');
  });
}

// The row the add control creates carries the mask its section needs. The section's rule rejects the
// empty string the server answers for a row saved without a mask, so a row created without one means
// one thing as a new row (no mask at all: the rule passes it) and another as a saved row (the empty
// string: the rule rejects it). The default is the token pair the fixture's rows carry, with the
// separator the mask matching style uses.
test('the dirwatch row the add control creates carries the mask the section needs, and Directory is its only complaint', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-dirwatch');

  addRow(el);

  const row = rows(el)[0];

  assert.equal(controlOf(row, 'Mask').value, '#SYS_#TG', 'the Mask field carries the mask the add control creates');

  const maskMessage = findAll(field(row, 'Mask'), (node) => node.className?.split(/\s+/).includes('rdio-form-error'))[0]?.textContent ?? '';

  assert.equal(maskMessage, '', 'the Mask field says nothing about the mask the row carries');
  assert.ok(marker(row), 'the row shows its warning icon for the Directory nobody filled in');
});
