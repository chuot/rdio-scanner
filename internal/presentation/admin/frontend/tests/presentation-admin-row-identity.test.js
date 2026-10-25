// unsaved rows stay themselves: the open panel and the revealed messages belong to the row
// rows are named by the order the section presents them: last add first, saved rows last

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

import { connectFields, fieldMessage, findAll, reachField, texts } from './dom-shim.js';

const METRO = { id: 1, label: 'Metro', order: 2, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };
const COUNTY = { id: 2, label: 'County', order: 1, systemRef: 2, blacklists: '', sites: [], talkgroups: [], units: [] };

const SITES = {
  1: [{ id: 6, label: 'North', siteRef: 60, order: 1 }],
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

const GROUPS = [{ id: 1, label: 'Dispatch' }];
const TAGS = [{ id: 3, label: 'Fire' }];

const ACCESS = [{ id: 1, order: 1, code: 'alpha', ident: 'AAA', systems: '*' }];
const APIKEYS = [{ id: 1, order: 1, key: 'first-key', ident: 'K1', systems: '*', disabled: false }];
const DOWNSTREAMS = [{ id: 1, order: 1, url: 'https://first.example', apikey: 'dk-1', systems: '*', disabled: false }];
const DIRWATCH = [{ id: 1, order: 1, directory: '/var/spool/first', extension: 'mp3', mask: '#SYS #TG', type: 'default', systemId: 2, siteId: null, talkgroupId: null, frequency: 1, delay: 2000, disabled: false }];

function slice(baseline) {
  return { baseline, working: structuredClone(baseline), dirty: false, invalid: false, conflict: false };
}

function seed(store) {
  const state = { auth: { authenticated: true, docker: false, passwordNeedChange: false }, alerts: {} };

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

function addRow(el) {
  control(el, 'add').click();
  connectFields(el);

  return rows(el)[0];
}

// the rows on the page, in the order the section presents them
function rows(el) {
  return findAll(el, (node) => node.tag === 'details'
    || (node.tag === 'div' && node.className?.split(/\s+/).includes('rdio-admin-row')));
}

// a field answers to its <label>, or to the field-title row beside it
function fieldTitle(node) {
  return texts(findAll(node, (child) => child.tag === 'label'))[0]
    ?? findAll(node.parent, (child) => child.className?.split(/\s+/).includes('field-title'))[0]?.textContent;
}

function field(row, title) {
  return findAll(row, (node) => node.tag === 'rdio-form-field').find((node) => fieldTitle(node) === title);
}

function controlOf(row, title) {
  return findAll(field(row, title), (node) => ['input', 'textarea', 'rdio-select'].includes(node.tag))[0];
}

// Every message the row carries, named by the field that carries it.
function messages(row) {
  return findAll(row, (node) => node.tag === 'rdio-form-field')
    .map((node) => ({ title: fieldTitle(node), message: fieldMessage(node) }))
    .filter((entry) => entry.message);
}

// The operator types in the field and the change fires the way leaving the field fires it.
function typeIn(row, title, value) {
  const control = controlOf(row, title);

  control.value = value;
  control.dispatchEvent({ type: 'change' });
  connectFields(row);

  return control;
}

// The operator edits the field and walks away: the change the browser fires as they leave it.
function leave(row, title) {
  reachField(controlOf(row, title));
}

function rowButton(row, label) {
  return findAll(row, (node) => node.tag === 'button' && node.textContent === label)[0];
}

// The drag handle's own events, fired on the list that carries the sort handlers.
function dragRow(el, from, to) {
  const list = findAll(el, (node) => node.className === 'rdio-admin-rows')[0];
  const moved = list.querySelectorAll(':scope > [data-drag]');
  const dataTransfer = {};
  const fire = (node, type) => list.dispatchEvent({ type, target: node, dataTransfer, preventDefault() {} });

  fire(moved[from], 'dragstart');
  fire(moved[to], 'dragover');
  fire(moved[to], 'drop');
  fire(moved[from], 'dragend');
}

// activation flips the row's open state and fires the toggle event
function activate(row) {
  row.open = !row.open;
  row.dispatchEvent({ type: 'toggle', target: row });
}

// The rows the operator sees open.
function openRows(el) {
  return rows(el).filter((row) => row.open);
}

// the systems tree: the systems section and the three per-level sections
// each section opens the row its add creates, and closes the rest
const TREE = [
  ['talkgroup', 'rdio-admin-talkgroups', 'Delete talkgroup'],
  ['site', 'rdio-admin-sites', 'Delete site'],
  ['unit', 'rdio-admin-units', 'Delete unit'],
  ['system', 'rdio-admin-systems', 'Delete system'],
];

for (const [name, tag, deleteLabel] of TREE) {
  // a second add must not move a revealed message onto an untouched row
  test(`a second new ${name} row keeps the message the operator revealed on the first new row`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);
    const first = addRow(el);

    leave(first, 'Label');

    assert.equal(fieldMessage(field(first, 'Label')), 'Label is required', 'the row the operator reached carries its message');

    addRow(el);

    const [second, firstRow] = rows(el);

    assert.equal(firstRow.open, false, 'the row the operator left open closes when the add control creates its row');
    assert.equal(second.open, true, 'the row the add control creates is the section’s one open row');
    assert.equal(openRows(el).length, 1, 'the list holds exactly one open row after the add');
    assert.equal(fieldMessage(field(firstRow, 'Label')), 'Label is required', 'the message stays on the row the operator reached, collapsed as it is');
    assert.deepEqual(messages(second), [], 'the row the second add created says nothing about fields nobody reached');
  });

  // a row added after a delete inherits nothing from the row it replaces
  test(`a ${name} row added after deleting a new row is a row of its own`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);
    const row = addRow(el);

    leave(row, 'Label');

    assert.equal(fieldMessage(field(row, 'Label')), 'Label is required');

    rowButton(row, deleteLabel).click();
    connectFields(el);

    const fresh = addRow(el);

    assert.deepEqual(messages(fresh), [], 'the new row inherits no message from the row the operator deleted');
    assert.equal(fresh.open, true, 'the new row opens as its section opens a row, and inherits nothing');
    assert.equal(openRows(el).length, 1, 'the row the add control creates is the section’s one open row');

    for (const saved of rows(el).slice(1)) {
      assert.equal(saved.open, false, 'no saved row opens on the state of the row the operator deleted');
      assert.deepEqual(messages(saved), [], 'no saved row carries a message of the row the operator deleted');
    }
  });

  // dragging the second row moves its revealed message with the row
  test(`dragging a new ${name} row moves the message with the row the operator reached`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);

    addRow(el);
    addRow(el);

    // only the second add's row is open, so this row is opened by hand first
    activate(rows(el)[1]);
    leave(rows(el)[1], 'Label');

    assert.equal(fieldMessage(field(rows(el)[1], 'Label')), 'Label is required', 'the row the operator reached carries its message');

    dragRow(el, 1, 0);

    assert.equal(fieldMessage(field(rows(el)[0], 'Label')), 'Label is required',
      'the message travels with the row the operator reached, not with the position it leaves');
    assert.deepEqual(messages(rows(el)[1]), [], 'the row nobody reached stays silent');
  });
}

// typed values stay in the row they were typed in across a second add
test('the second new site row leaves the label the operator typed in the first new row', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-sites');

  typeIn(addRow(el), 'Label', 'Depot');

  addRow(el);

  const [second, firstRow] = rows(el);

  assert.equal(texts(findAll(firstRow, (node) => node.className?.split(/\s+/).includes('row-title')))[0], 'Depot',
    'the row the operator typed in keeps its place and its label');
  assert.equal(controlOf(second, 'Label').value, '', 'the row the second add created is empty');

  const working = getAdminContext().store.getState()['subsection:sites:2'].working;

  assert.deepEqual(working.map((site) => site.label), ['', 'Depot', 'East'], 'the working copy holds both new rows, each with what the operator left in it');
});

// the six hand-built row sections: no open state, only the revealed message and its row

const HAND_BUILT = [
  ['groups', 'rdio-admin-groups', 'Label', 'Label is required', 'Delete Group'],
  ['tags', 'rdio-admin-tags', 'Label', 'Label is required', 'Delete Tag'],
  ['access', 'rdio-admin-access', 'Code', 'Code is required', 'Delete access'],
  ['API keys', 'rdio-admin-apikeys', 'Ident', 'Ident is required', 'Delete API key'],
  ['downstreams', 'rdio-admin-downstreams', 'URL', 'URL is required', 'Delete downstream'],
  ['dirwatch', 'rdio-admin-dirwatch', 'Directory', 'Directory is required', 'Delete dirwatch'],
];

for (const [name, tag, title, message, deleteLabel] of HAND_BUILT) {
  test(`a second new ${name} row keeps the message the operator revealed on the first new row`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);
    const first = addRow(el);

    typeIn(first, title, '');

    assert.equal(fieldMessage(field(first, title)), message, 'the row the operator reached carries its message');

    addRow(el);

    const [second, firstRow] = rows(el);

    assert.equal(fieldMessage(field(firstRow, title)), message, 'the message stays on the row the operator reached');
    assert.deepEqual(messages(second), [], 'the row the second add created says nothing about fields nobody reached');
  });

  test(`a new ${name} row added after deleting a new row inherits nothing from it`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);
    const row = addRow(el);

    typeIn(row, title, '');

    assert.equal(fieldMessage(field(row, title)), message);

    rowButton(row, deleteLabel).click();
    connectFields(el);

    const fresh = addRow(el);

    assert.deepEqual(messages(fresh), [], 'the new row inherits no message from the row the operator deleted');

    for (const saved of rows(el).slice(1)) {
      assert.deepEqual(messages(saved), [], 'no saved row carries a message of the row the operator deleted');
    }
  });

  test(`dragging a new ${name} row moves the message with the row the operator reached`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);

    addRow(el);
    addRow(el);

    typeIn(rows(el)[1], title, '');

    assert.equal(fieldMessage(field(rows(el)[1], title)), message, 'the row the operator reached carries its message');

    dragRow(el, 1, 0);

    assert.equal(fieldMessage(field(rows(el)[0], title)), message,
      'the message travels with the row the operator reached, not with the position it leaves');
    assert.deepEqual(messages(rows(el)[1]), [], 'the row nobody reached stays silent');
  });
}
