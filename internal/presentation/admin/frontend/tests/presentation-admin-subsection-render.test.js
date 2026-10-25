// these tests read the rendered rows, not the store's working copy
// cases are the four row changes: add, delete, system switch, server replacement

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

// the save bar renders only when attached: the shim attaches it for saveButton
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

// sets the value, focuses the input, and fires change as leaving the field does
function typeIn(input, value) {
  input.value = value;
  input.focus();
  input.dispatchEvent({ type: 'change' });
}

// the list carries the drag listeners: events target the moved and landing rows
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

// the other two levels: the section follows its own slice, whatever the level is.
test('the talkgroup section shows the rows the server pushes for the system it is showing', () => {
  const store = seed();
  const el = attach('rdio-admin-talkgroups');
  assert.deepEqual(rowTitles(el), ['Fire']);

  pushRows(store, 'talkgroups', 2, [
    { id: 7, label: 'Fire', name: 'Fire dispatch', talkgroupRef: 70, order: 1, groupIds: [1], tagId: 3, alert: '', delay: null, frequency: null, led: '', type: '' },
    { id: 12, label: 'Talk', name: 'Talk dispatch', talkgroupRef: 12, order: 2, groupIds: [1], tagId: 3, alert: '', delay: null, frequency: null, led: '', type: '' },
  ]);

  assert.deepEqual(rowTitles(el), ['Fire', 'Talk'], 'rows the server added must appear without a reload');
});

test('picking another system shows that system’s talkgroups, and the add control works there', () => {
  seed();
  const el = attach('rdio-admin-talkgroups');
  assert.deepEqual(rowTitles(el), ['Fire']);

  pickSystem(el, 1);
  assert.deepEqual(rowTitles(el), [], 'Metro has no saved talkgroups');

  addRow(el);
  assert.deepEqual(rowTitles(el), ['NewTalkgroup'], 'the new row belongs to the system now showing');

  const { store } = getAdminContext();
  assert.deepEqual(sliceFor(store, 'talkgroups', 1).working.map((row) => row.id), [0],
    'the empty row lands in the slice of the system now showing');
  assert.deepEqual(sliceFor(store, 'talkgroups', 2).working.map((row) => row.label), ['Fire'],
    'the system left behind keeps its own rows');
});

test('pressing Delete talkgroup removes the row from the rendered list', () => {
  seed();
  const el = attach('rdio-admin-talkgroups');
  addRow(el);
  assert.deepEqual(rowTitles(el), ['NewTalkgroup', 'Fire']);

  rowButton(rows(el)[0], 'Delete talkgroup').click();

  assert.deepEqual(rowTitles(el), ['Fire'], 'the row the operator deleted must leave the page');
});

test('the talkgroup section falls back to another system when the one it shows is deleted', () => {
  const store = seed();
  const el = attach('rdio-admin-talkgroups');
  assert.deepEqual(rowTitles(el), ['Fire']);

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
  assert.deepEqual(rowTitles(el), ['NewTalkgroup'],
    'after falling back, the add control renders its row on the system now showing');
});

test('the unit section shows the rows the server pushes for the system it is showing', () => {
  const store = seed();
  const el = attach('rdio-admin-units');
  assert.deepEqual(rowTitles(el), ['Car']);

  pushRows(store, 'units', 2, [
    { id: 9, label: 'Car', unitRef: 100, unitFrom: 100, unitTo: 199, order: 1 },
    { id: 13, label: 'Truck', unitRef: 13, unitFrom: 130, unitTo: 139, order: 2 },
  ]);

  assert.deepEqual(rowTitles(el), ['Car', 'Truck'], 'rows the server added must appear without a reload');
});

test('picking another system shows that system’s units, and the add control works there', () => {
  seed();
  const el = attach('rdio-admin-units');
  assert.deepEqual(rowTitles(el), ['Car']);

  pickSystem(el, 1);
  assert.deepEqual(rowTitles(el), [], 'Metro has no saved units');

  addRow(el);
  assert.deepEqual(rowTitles(el), ['NewUnit'], 'the new row belongs to the system now showing');

  const { store } = getAdminContext();
  assert.deepEqual(sliceFor(store, 'units', 1).working.map((row) => row.id), [0],
    'the empty row lands in the slice of the system now showing');
  assert.deepEqual(sliceFor(store, 'units', 2).working.map((row) => row.label), ['Car'],
    'the system left behind keeps its own rows');
});

test('pressing Delete unit removes the row from the rendered list', () => {
  seed();
  const el = attach('rdio-admin-units');
  addRow(el);
  assert.deepEqual(rowTitles(el), ['NewUnit', 'Car']);

  rowButton(rows(el)[0], 'Delete unit').click();

  assert.deepEqual(rowTitles(el), ['Car'], 'the row the operator deleted must leave the page');
});

test('the unit section falls back to another system when the one it shows is deleted', () => {
  const store = seed();
  const el = attach('rdio-admin-units');
  assert.deepEqual(rowTitles(el), ['Car']);

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
  assert.deepEqual(rowTitles(el), ['NewUnit'],
    'after falling back, the add control renders its row on the system now showing');
});

// editing a field keeps the row and field on the page: typing rebuilds nothing
// the Systems section does this; these cases hold the per-level sections to it
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

// a field edit does not rebuild the rows the render built
// so a drag commits the rows as the operator left them in the fields
// see the drag-commits-edited-rows maintainer note.
test('dragging a site after editing it keeps what the operator typed', () => {
  const store = seed();
  const el = attach('rdio-admin-sites');

  addRow(el);
  assert.deepEqual(rowTitles(el), ['NewSite', 'East']);

  const row = rows(el)[0];
  typeIn(fieldInput(row, 'Id'), '999');
  typeIn(fieldInput(row, 'Label'), 'Depot');

  dragRow(el, 0, 1);

  assert.deepEqual(rowTitles(el), ['East', 'Depot'],
    'the row the operator moved keeps the label they typed, and lands below the saved row');
  assert.deepEqual(sliceFor(store, 'sites', 2).working.map((site) => [site.siteRef, site.label]), [[40, 'East'], [999, 'Depot']],
    'the move commits the rows as the operator left them');
});

test('dragging a talkgroup after editing it keeps what the operator typed', () => {
  const store = seed();
  const el = attach('rdio-admin-talkgroups');

  addRow(el);
  assert.deepEqual(rowTitles(el), ['NewTalkgroup', 'Fire']);

  const row = rows(el)[0];
  typeIn(fieldInput(row, 'Id'), '888');
  typeIn(fieldInput(row, 'Label'), 'Dispatch');

  dragRow(el, 0, 1);

  assert.deepEqual(rowTitles(el), ['Fire', 'Dispatch'],
    'the row the operator moved keeps the label they typed, and lands below the saved row');
  assert.deepEqual(sliceFor(store, 'talkgroups', 2).working.map((talkgroup) => [talkgroup.talkgroupRef, talkgroup.label]), [[70, 'Fire'], [888, 'Dispatch']],
    'the move commits the rows as the operator left them');
});

// typing never closes the one open row: the field stays on the page
test('typing in a field of the open row leaves the section with that one row open', () => {
  const store = seed();
  const el = attach('rdio-admin-sites');

  addRow(el);

  const open = rows(el)[0];

  assert.equal(rows(el).filter((row) => row.open).length, 1, 'the row the add control created is the section’s one open row');

  typeIn(fieldInput(open, 'Id'), '999');
  typeIn(fieldInput(open, 'Label'), 'Depot');

  assert.equal(rows(el).filter((row) => row.open).length, 1, 'the keystroke closes no row: the section still shows one open row');
  assert.equal(rows(el)[0], open, 'the row the operator is typing in stays open on the page');
  assert.equal(sliceFor(store, 'sites', 2).working[0].label, 'Depot', 'the section holds what the operator typed');
  assert.equal(saveButton(el).disabled, false, 'Save answers the change the operator made, not the rows they opened');
});
