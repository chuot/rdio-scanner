// The rows the operator added and has not saved. The section remembers each of them as itself: the
// open panel and the messages the operator revealed belong to the row they worked on, and not to the
// position a render happens to leave that row in.
//
// The readings are taken from the rendered rows — the panel that is open, and the text under the
// control. The rows are named by the order the section presents them: the row the last add created is
// the first, the row the first add created follows it, and the saved rows come after.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-systems.js';
import '../presentation/admin/rdio-admin-sites.js';
import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-units.js';

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

// The rows on the page, in the order the section presents them.
function rows(el) {
  return findAll(el, (node) => node.tag === 'details');
}

function field(row, title) {
  return findAll(row, (node) => node.tag === 'rdio-form-field')
    .find((node) => texts(findAll(node, (child) => child.tag === 'label'))[0] === title);
}

function controlOf(row, title) {
  return findAll(field(row, title), (node) => ['input', 'textarea', 'rdio-select'].includes(node.tag))[0];
}

// Every message the row carries, named by the field that carries it.
function messages(row) {
  return findAll(row, (node) => node.tag === 'rdio-form-field')
    .map((node) => ({ title: texts(findAll(node, (child) => child.tag === 'label'))[0], message: fieldMessage(node) }))
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

// The systems tree: the systems section and the three per-level sections. Each row the operator adds
// is filled in the same way, and each section's delete control names its own row. The per-level
// sections open the row their add control creates; the systems section presents a new row closed, and
// there it is the operator who opens the row they came to fill in.
const TREE = [
  ['talkgroup', 'rdio-admin-talkgroups', 'Delete talkgroup', true],
  ['site', 'rdio-admin-sites', 'Delete site', true],
  ['unit', 'rdio-admin-units', 'Delete unit', true],
  ['system', 'rdio-admin-systems', 'Delete system', false],
];

for (const [name, tag, deleteLabel, opensOnAdd] of TREE) {
  // The reading the operator named: they came to fill in a row, they reached a field of it, and the
  // add control for a second row moved that row's state onto the row they never touched.
  test(`a second new ${name} row leaves the first new row open, with the message the operator revealed`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);
    const first = addRow(el);

    if (!opensOnAdd) first.open = true;

    leave(first, 'Label');

    assert.equal(fieldMessage(field(first, 'Label')), 'Label is required', 'the row the operator reached carries its message');

    addRow(el);

    const [second, firstRow] = rows(el);

    assert.equal(firstRow.open, true, 'the row the operator left open stays open');
    assert.equal(fieldMessage(field(firstRow, 'Label')), 'Label is required', 'the message stays on the row the operator reached');
    assert.deepEqual(messages(second), [], 'the row the second add created says nothing about fields nobody reached');

    if (opensOnAdd) assert.equal(second.open, true, 'the add control opens the row it creates');
  });

  // The row the operator deleted was open, and its messages were revealed. The row the add control
  // creates next is a row of its own: it says nothing, and it opens only as its section opens a row.
  test(`a ${name} row added after deleting a new row is a row of its own`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);
    const row = addRow(el);

    if (!opensOnAdd) row.open = true;

    leave(row, 'Label');

    assert.equal(fieldMessage(field(row, 'Label')), 'Label is required');

    rowButton(row, deleteLabel).click();
    connectFields(el);

    const fresh = addRow(el);

    assert.deepEqual(messages(fresh), [], 'the new row inherits no message from the row the operator deleted');
    assert.equal(fresh.open, opensOnAdd, 'the new row opens as its section opens a row, and inherits nothing');

    for (const saved of rows(el).slice(1)) {
      assert.equal(saved.open, false, 'no saved row opens on the state of the row the operator deleted');
      assert.deepEqual(messages(saved), [], 'no saved row carries a message of the row the operator deleted');
    }
  });

  // The row the operator reached is the second row on the page. Dragging it above the row the last
  // add created moves the row, and the message travels with it.
  test(`dragging a new ${name} row moves the message with the row the operator reached`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);

    addRow(el);
    addRow(el);

    leave(rows(el)[1], 'Label');

    assert.equal(fieldMessage(field(rows(el)[1], 'Label')), 'Label is required', 'the row the operator reached carries its message');

    dragRow(el, 1, 0);

    assert.equal(fieldMessage(field(rows(el)[0], 'Label')), 'Label is required',
      'the message travels with the row the operator reached, not with the position it leaves');
    assert.deepEqual(messages(rows(el)[1]), [], 'the row nobody reached stays silent');
  });
}

// The identity is the row's own, so the values the operator typed stay in the row they typed them in
// across the second add: the reading the sections' own row-staying behaviour already promises.
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
