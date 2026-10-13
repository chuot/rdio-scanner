// What the operator sees in the per-level sections. The store's working copy is not the screen:
// these tests read the rendered rows, so a section that updates its data and leaves the page
// untouched fails here.
//
// Every case is one of the four ways the rows change: the operator presses the add control, the
// operator presses delete, the operator picks another system, and the server replaces the level
// under the operator's feet.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-sites.js';
import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-units.js';

import { findAll, texts } from './dom-shim.js';

const METRO = { id: 1, label: 'Metro', order: 2, blacklists: '', sites: [], talkgroups: [], units: [] };
const COUNTY = { id: 2, label: 'County', order: 1, blacklists: '', sites: [], talkgroups: [], units: [] };

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

const GROUPS = [{ id: 1, label: 'Dispatch' }, { id: 2, label: 'Tac' }];
const TAGS = [{ id: 3, label: 'Fire' }];

function systemsSection() {
  return {
    baseline: [METRO, COUNTY],
    working: [METRO, COUNTY],
    dirty: false,
    invalid: false,
    conflict: false,
  };
}

function seed() {
  const { store } = getAdminContext();

  const state = { auth: { authenticated: true, docker: false, passwordNeedChange: false } };

  for (const [level, bySystem] of [['sites', SITES], ['talkgroups', TALKGROUPS], ['units', UNITS]]) {
    for (const system of [COUNTY, METRO]) {
      state[`subsection:${level}:${system.id}`] = {
        baseline: structuredClone(bySystem[system.id]),
        working: structuredClone(bySystem[system.id]),
        dirty: false,
        invalid: false,
        conflict: false,
      };
    }
  }

  store.setState({
    ...state,
    'section:systems': systemsSection(),
    'section:groups': { baseline: GROUPS, working: GROUPS, dirty: false, invalid: false, conflict: false },
    'section:tags': { baseline: TAGS, working: TAGS, dirty: false, invalid: false, conflict: false },
    alerts: {},
  });

  return store;
}

function attach(tag) {
  const el = new (customElements.get(tag))();
  el.connectedCallback();
  return el;
}

function rows(el) {
  return findAll(el, (node) => node.tag === 'details');
}

function rowTitles(el) {
  return texts(findAll(el, (node) => node.className === 'row-title'));
}

function fieldLabels(row) {
  return texts(findAll(row, (node) => node.tag === 'label'));
}

function control(el, className) {
  return findAll(el, (node) => node.tag === 'button' && node.className.split(/\s+/).includes(className))[0];
}

function rowButton(row, label) {
  return findAll(row, (node) => node.tag === 'button' && node.textContent === label)[0];
}

function dropdown(el) {
  return findAll(el, (node) => node.tag === 'rdio-select')[0];
}

// The save bar is its own element that renders when it is attached to the page; the shim
// attaches it here so a test can read the Save control the operator sees.
function saveButton(el) {
  const bar = findAll(el, (node) => node.tag === 'rdio-admin-section-bar')[0];

  bar.connectedCallback();

  return findAll(bar, (node) => node.tag === 'button' && node.className.split(/\s+/).includes('save'))[0];
}

function pickSystem(el, id) {
  dropdown(el).dispatchEvent(new CustomEvent('change', { detail: String(id) }));
}

function addRow(el) {
  control(el, 'add').click();
}

function sliceFor(store, level, systemId) {
  return store.getState()[`subsection:${level}:${systemId}`];
}

function fieldInput(row, title) {
  const field = findAll(row, (node) => node.tag === 'rdio-form-field')
    .find((node) => texts(findAll(node, (child) => child.tag === 'label'))[0] === title);

  return findAll(field, (node) => node.tag === 'input')[0];
}

// The operator types in the field and moves on: the value lands in the input, the
// input has the focus, and the change event fires the way leaving the field fires it.
function typeIn(input, value) {
  input.value = value;
  input.focus();
  input.dispatchEvent({ type: 'change' });
}

function pushRows(store, level, systemId, rows) {
  const slice = sliceFor(store, level, systemId);
  store.setState({
    [`subsection:${level}:${systemId}`]: {
      ...slice,
      baseline: rows.map((row) => ({ ...row })),
      working: rows.map((row) => ({ ...row })),
      dirty: false,
      invalid: false,
      conflict: false,
    },
  });
}

test('pressing New Site renders the new row above the saved sites, open on its fields', () => {
  seed();
  const el = attach('rdio-admin-sites');
  assert.deepEqual(rowTitles(el), ['East'], 'the section opens on the saved rows of the system it shows');

  addRow(el);

  assert.deepEqual(rowTitles(el), ['NewSite', 'East'],
    'the row the operator added must appear on the page, not only in the store');
  const [top] = rows(el);
  assert.equal(top.open, true, 'the new row must be open so its fields are presented');
  assert.deepEqual(fieldLabels(top), ['Id', 'Label']);
});

test('pressing New Talkgroup renders the new row above the saved talkgroups, open on its fields', () => {
  seed();
  const el = attach('rdio-admin-talkgroups');
  assert.deepEqual(rowTitles(el), ['Fire']);

  addRow(el);

  assert.deepEqual(rowTitles(el), ['NewTalkgroup', 'Fire']);
  const [top] = rows(el);
  assert.equal(top.open, true);
  const labels = fieldLabels(top);
  for (const field of ['Id', 'Label', 'Name', 'Tag', 'Audio alert', 'Delay', 'Frequency', 'Led Color', 'Type']) {
    assert.ok(labels.includes(field), `the talkgroup row is missing its ${field} field`);
  }
  assert.ok(texts(findAll(top, (node) => node.className === 'body')).includes('Groups'),
    'the talkgroup row presents the Groups list');
  const groupsList = findAll(top, (node) => node.className === 'rdio-checkbox-list')[0];
  assert.ok(groupsList, 'the talkgroup row presents the Groups list');
  assert.deepEqual(texts(findAll(groupsList, (node) => node.tag === 'span')), ['Dispatch', 'Tac'],
    'the Groups list offers the groups from the Groups section');
});

test('pressing New Unit renders the new row above the saved units, open on its fields', () => {
  seed();
  const el = attach('rdio-admin-units');
  assert.deepEqual(rowTitles(el), ['Car']);

  addRow(el);

  assert.deepEqual(rowTitles(el), ['NewUnit', 'Car']);
  const [top] = rows(el);
  assert.equal(top.open, true);
  assert.deepEqual(fieldLabels(top), ['Id', 'Range from', 'Range to', 'Label']);
});

test('the section shows the rows the server pushes for the level it is showing', () => {
  const store = seed();
  const el = attach('rdio-admin-sites');
  assert.deepEqual(rowTitles(el), ['East']);

  // A configuration arriving from the server replaces the level under the operator's feet.
  pushRows(store, 'sites', 2, [
    { id: 4, label: 'East', siteRef: 40, order: 1 },
    { id: 11, label: 'Depot', siteRef: 11, order: 2 },
  ]);

  assert.deepEqual(rowTitles(el), ['East', 'Depot'],
    'rows the server added must appear without a reload');
});

test('picking another system shows that system’s rows, and the add control works there', () => {
  seed();
  const el = attach('rdio-admin-sites');
  assert.deepEqual(rowTitles(el), ['East']);

  pickSystem(el, 1);
  assert.deepEqual(rowTitles(el), [], 'Metro has no saved sites');

  addRow(el);
  assert.deepEqual(rowTitles(el), ['NewSite'], 'the new row belongs to the system now showing');

  const { store } = getAdminContext();
  assert.deepEqual(sliceFor(store, 'sites', 1).working.map((row) => row.id), [0],
    'the empty row lands in the slice of the system now showing');
  assert.deepEqual(sliceFor(store, 'sites', 2).working.map((row) => row.label), ['East'],
    'the system left behind keeps its own rows');
});

test('pressing Delete site removes the row from the rendered list', () => {
  seed();
  const el = attach('rdio-admin-sites');
  addRow(el);
  assert.deepEqual(rowTitles(el), ['NewSite', 'East']);

  rowButton(rows(el)[0], 'Delete site').click();

  assert.deepEqual(rowTitles(el), ['East'], 'the row the operator deleted must leave the page');
});

test('the section falls back to another system when the one it shows is deleted', () => {
  const store = seed();
  const el = attach('rdio-admin-sites');
  assert.deepEqual(rowTitles(el), ['East']);

  store.setState({
    'section:systems': {
      baseline: [METRO],
      working: [METRO],
      dirty: false,
      invalid: false,
      conflict: false,
    },
  });

  assert.deepEqual(rowTitles(el), [], 'the section re-renders on the system it falls back to');
  addRow(el);
  assert.deepEqual(rowTitles(el), ['NewSite'],
    'after falling back, the add control renders its row on the system now showing');
});

// CA-003: a field edit is the operator's own typing, not a reason to rebuild the
// rows they are working on. The Systems section already behaves this way; these
// three cases hold the per-level sections to it.
test('editing a site label keeps the row and the field the operator is in on the page', () => {
  const store = seed();
  const el = attach('rdio-admin-sites');

  const row = rows(el)[0];
  const input = fieldInput(row, 'Label');

  typeIn(input, 'Depot');

  assert.equal(rows(el)[0], row, 'the row the operator is editing stays on the page');
  assert.equal(findAll(el, (node) => node === input).length, 1, 'the field the operator typed in is the same node');
  assert.equal(input.focused, true, 'the operator keeps the focus they had in the field');
  assert.equal(sliceFor(store, 'sites', 2).working[0].label, 'Depot', 'the section holds what the operator typed');
  assert.equal(saveButton(el).disabled, false, 'Save is enabled for the change the operator made');
});

test('editing a talkgroup label keeps the row and the field the operator is in on the page', () => {
  const store = seed();
  const el = attach('rdio-admin-talkgroups');

  const row = rows(el)[0];
  const input = fieldInput(row, 'Label');

  typeIn(input, 'Fire Dispatch');

  assert.equal(rows(el)[0], row, 'the row the operator is editing stays on the page');
  assert.equal(findAll(el, (node) => node === input).length, 1, 'the field the operator typed in is the same node');
  assert.equal(input.focused, true, 'the operator keeps the focus they had in the field');
  assert.equal(sliceFor(store, 'talkgroups', 2).working[0].label, 'Fire Dispatch', 'the section holds what the operator typed');
  assert.equal(saveButton(el).disabled, false, 'Save is enabled for the change the operator made');
});

test('editing a unit label keeps the row and the field the operator is in on the page', () => {
  const store = seed();
  const el = attach('rdio-admin-units');

  const row = rows(el)[0];
  const input = fieldInput(row, 'Label');

  typeIn(input, 'Car 1');

  assert.equal(rows(el)[0], row, 'the row the operator is editing stays on the page');
  assert.equal(findAll(el, (node) => node === input).length, 1, 'the field the operator typed in is the same node');
  assert.equal(input.focused, true, 'the operator keeps the focus they had in the field');
  assert.equal(sliceFor(store, 'units', 2).working[0].label, 'Car 1', 'the section holds what the operator typed');
  assert.equal(saveButton(el).disabled, false, 'Save is enabled for the change the operator made');
});
