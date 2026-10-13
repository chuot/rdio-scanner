// The field messages of the Systems, Sites, Talkgroups, and Units sections, read the way the
// operator reads them: the messages a row the operator just added carries, the messages that appear
// when the operator reaches a field, and the message a field keeps after the operator leaves a good
// value in it.
//
// The readings are taken from the text under the control, written by the form field primitive
// itself, and the rows are the rendered rows — the store's working copy is not the screen.

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

  return findAll(el, (node) => node.tag === 'details')[0];
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

// A select is picked by the primitive's own event: the value leaves the select as a CustomEvent.
function pickIn(row, title, value) {
  findAll(row, (node) => node.tag === 'rdio-select')
    .find((node) => texts(findAll(node.parent, (child) => child.tag === 'label'))[0] === title)
    ?.dispatchEvent(new CustomEvent('change', { detail: value }));

  connectFields(row);
}

// The operator ticks one box of the Groups list: the box is checked, and the change fires the way
// leaving the box fires it.
function checkBox(row) {
  const box = findAll(row, (node) => node.tag === 'input' && node.type === 'checkbox')[0];

  box.checked = true;
  box.dispatchEvent({ type: 'change' });
  connectFields(row);
}

function marker(row) {
  return findAll(row, (node) => node.className?.split(/\s+/).includes('row-invalid'))[0] ?? null;
}

// The save bar is its own element that renders when it is attached to the page.
function barButton(el, className) {
  const bar = findAll(el, (node) => node.tag === 'rdio-admin-section-bar')[0];

  bar.connectedCallback();

  return findAll(bar, (node) => node.tag === 'button' && node.className.split(/\s+/).includes(className))[0];
}

function saveButton(el) {
  return barButton(el, 'save');
}

// The operator edits the field and walks away: the change the browser fires as they leave it.
function leave(row, title) {
  reachField(controlOf(row, title));
}

// ---------------------------------------------------------------------------
// The talkgroup row the operator came to fill in.
// ---------------------------------------------------------------------------

test('a new talkgroup row says nothing about fields the operator has not reached', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-talkgroups'));

  assert.deepEqual(messages(row), []);
});

test('a new talkgroup row is marked as the invalid row it is', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-talkgroups'));

  assert.ok(marker(row), 'the row carries its warning icon');
});

test('leaving a field of a new talkgroup row reveals the message its empty value deserves', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-talkgroups');
  const row = addRow(el);

  leave(row, 'Id');

  assert.equal(fieldMessage(field(row, 'Id')), 'Id is required');
  assert.equal(controlOf(row, 'Id').getAttribute('aria-invalid'), 'true', 'the control the message is written under says it is invalid');
});

test('walking across a field of a new talkgroup row reveals nothing', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-talkgroups'));

  // The operator tabbed through the row: the field took the focus and gave it up, and its value
  // never changed. Material's matcher read that as touched, not dirty, and showed no message.
  const control = controlOf(row, 'Label');

  control.focus();
  control.dispatchEvent({ type: 'focusout' });

  assert.deepEqual(messages(row), [], 'a field the operator only walked over says nothing');

  const marked = findAll(row, (node) => node.tag === 'rdio-form-field')
    .map((node) => ({
      title: texts(findAll(node, (child) => child.tag === 'label'))[0],
      invalid: findAll(node, (child) => ['input', 'textarea', 'select'].includes(child.tag))[0]?.getAttribute('aria-invalid') === 'true',
    }))
    .filter((entry) => entry.invalid)
    .map((entry) => entry.title);

  assert.deepEqual(marked, [], 'no control of a row nobody edited is marked invalid');
});

test('picking in the Tag select of a new talkgroup row reveals that a tag is required', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-talkgroups'));

  pickIn(row, 'Tag', '');

  assert.equal(fieldMessage(field(row, 'Tag')), 'Tag is required');
});

test('a good Label clears the message the Label field was showing', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-talkgroups'));

  leave(row, 'Label');

  assert.equal(fieldMessage(field(row, 'Label')), 'Label is required');

  typeIn(row, 'Label', 'Fire Dispatch');

  assert.equal(fieldMessage(field(row, 'Label')), '');
  assert.equal(controlOf(row, 'Label').getAttribute('aria-invalid'), null, 'the mark goes away with the message');
});

test('clearing the Label brings its message back', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-talkgroups'));

  typeIn(row, 'Label', 'Fire Dispatch');
  typeIn(row, 'Label', '');

  assert.equal(fieldMessage(field(row, 'Label')), 'Label is required');
});

test('an identifier the row already has is reported on the field that carries it', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-talkgroups');

  el.systemId = COUNTY.id;
  el.render();

  const row = addRow(el);

  typeIn(row, 'Id', '70');

  assert.equal(fieldMessage(field(row, 'Id')), 'Id is already defined');
});

test('the row loses its warning icon once its fields carry valid values', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-talkgroups');

  el.systemId = COUNTY.id;
  el.render();

  const row = addRow(el);

  assert.ok(marker(row));

  typeIn(row, 'Id', '71');
  typeIn(row, 'Label', 'Car 21');
  typeIn(row, 'Name', 'Car 21 dispatch');

  checkBox(row);
  pickIn(row, 'Tag', '3');

  assert.equal(marker(row), null);
});

test('the Save control stays disabled while the new row is invalid and lights when it is valid', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-talkgroups');

  el.systemId = COUNTY.id;
  el.render();

  const row = addRow(el);

  assert.equal(saveButton(el).disabled, true);

  typeIn(row, 'Id', '71');
  typeIn(row, 'Label', 'Car 21');
  typeIn(row, 'Name', 'Car 21 dispatch');
  checkBox(row);
  pickIn(row, 'Tag', '3');

  assert.equal(saveButton(el).disabled, false);
});

test('the field the operator edited is the same field node, holding what they typed and the message its new value deserves', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-talkgroups');
  const row = addRow(el);

  leave(row, 'Label');

  const labelField = field(row, 'Label');
  const labelInput = controlOf(row, 'Label');

  labelInput.value = 'Fire Dispatch';
  labelInput.dispatchEvent({ type: 'change' });

  assert.equal(field(row, 'Label'), labelField, 'the field was not replaced under the operator');
  assert.equal(labelInput.focused !== true, true);
  assert.equal(getAdminContext().store.getState()[`subsection:talkgroups:${el.systemId}`].working[0].label, 'Fire Dispatch');
  assert.equal(fieldMessage(labelField), '');
});

// ---------------------------------------------------------------------------
// The same three readings in the other three sections the operator walks.
// ---------------------------------------------------------------------------

const SECTIONS = [
  ['Sites', 'rdio-admin-sites', 'Label', 'Central', 'Id', '41'],
  ['Units', 'rdio-admin-units', 'Label', 'Car 12', 'Id', '120'],
  ['Systems', 'rdio-admin-systems', 'Label', 'County West', 'Id', '3'],
];

for (const [name, tag, firstTitle, goodValue, idTitle, idValue] of SECTIONS) {
  test(`a new ${name.toLowerCase()} row says nothing about fields the operator has not reached`, () => {
    seed(getAdminContext().store);

    const row = addRow(attach(tag));

    assert.deepEqual(messages(row), []);
  });

  test(`leaving a field of a new ${name.toLowerCase()} row reveals its message, and a good value clears it`, () => {
    seed(getAdminContext().store);

    const row = addRow(attach(tag));

    leave(row, firstTitle);

    assert.equal(fieldMessage(field(row, firstTitle)), `${firstTitle} is required`);

    typeIn(row, firstTitle, goodValue);

    assert.equal(fieldMessage(field(row, firstTitle)), '');
  });

  test(`a new ${name.toLowerCase()} row is marked invalid, and the marker is gone when its fields are valid`, () => {
    seed(getAdminContext().store);

    const row = addRow(attach(tag));

    assert.ok(marker(row));

    typeIn(row, firstTitle, goodValue);
    typeIn(row, idTitle, idValue);

    assert.equal(marker(row), null);
  });
}

// ---------------------------------------------------------------------------
// The state of having reached a field, across the rebuilds the section does.
// ---------------------------------------------------------------------------

// The saved sites of both systems. Metro's site carries no identifier: it is a row the operator
// has not reached, and its message is the reading the section must keep to itself.
const SAVED_SITES = {
  1: [{ id: 6, label: 'North', siteRef: null, order: 1 }],
  2: [{ id: 4, label: 'East', siteRef: 40, order: 1 }, { id: 5, label: 'West', siteRef: 41, order: 2 }],
};

function setSlice(store, key, rows) {
  store.setState({ [key]: slice(rows) });

  return store;
}

function rowButton(row, label) {
  return findAll(row, (node) => node.tag === 'button' && node.textContent === label)[0];
}

function pickSystem(el, id) {
  findAll(el, (node) => node.tag === 'rdio-select')[0].dispatchEvent(new CustomEvent('change', { detail: String(id) }));
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

function sitesOnSavedRows() {
  const store = setSlice(setSlice(seed(getAdminContext().store), 'subsection:sites:1', SAVED_SITES[1]), 'subsection:sites:2', SAVED_SITES[2]);

  // The section picks the system it shows, the way it does in the application: County is the
  // system the dropdown lists first, and it is the one carrying the two saved sites.
  const el = attach('rdio-admin-sites');

  return { store, el };
}

test('the message a field carries survives every rebuild the section does', () => {
  const { el } = sitesOnSavedRows();

  // The operator edits East's identifier away from the value the server holds: the row is invalid,
  // the section is dirty, and the field they edited carries its message.
  typeIn(rows(el)[0], 'Id', '');

  assert.equal(fieldMessage(field(rows(el)[0], 'Id')), 'Id is required');

  addRow(el);

  assert.equal(fieldMessage(field(rows(el)[1], 'Id')), 'Id is required', 'adding a row above it keeps the message on the row it belongs to');

  rowButton(rows(el)[0], 'Delete site').click();

  assert.equal(fieldMessage(field(rows(el)[0], 'Id')), 'Id is required', 'deleting a row keeps the message on the row it belongs to');

  dragRow(el, 0, 1);

  assert.equal(fieldMessage(field(rows(el)[1], 'Id')), 'Id is required', 'dragging the rows moves the message with the row it belongs to');

  pickSystem(el, METRO.id);

  assert.deepEqual(messages(rows(el)[0]), [], 'the system the operator switched to says nothing about a field they have not reached there');

  pickSystem(el, COUNTY.id);

  assert.equal(fieldMessage(field(rows(el)[1], 'Id')), 'Id is required', 'switching back to the system the operator left dirty reveals the message again');
});

test('discarding the section leaves every field unreached', () => {
  const { el } = sitesOnSavedRows();

  typeIn(rows(el)[0], 'Id', '');

  assert.equal(fieldMessage(field(rows(el)[0], 'Id')), 'Id is required');

  barButton(el, 'discard').click();

  assert.deepEqual(messages(rows(el)[0]), [], 'the discarded section says nothing about a field the operator has not reached again');

  typeIn(rows(el)[0], 'Id', '');

  assert.equal(fieldMessage(field(rows(el)[0], 'Id')), 'Id is required', 'reaching the field reveals its message again');
});

test('a configuration push leaves every field unreached', () => {
  const { store, el } = sitesOnSavedRows();

  typeIn(rows(el)[0], 'Id', '');

  assert.equal(fieldMessage(field(rows(el)[0], 'Id')), 'Id is required');

  // The server's copy of the section, written over the operator's: baseline and working agree.
  setSlice(store, 'subsection:sites:2', SAVED_SITES[2].map((site) => ({ ...site })));

  assert.deepEqual(messages(rows(el)[0]), [], 'the pushed section says nothing about a field the operator has not reached again');
});

// ---------------------------------------------------------------------------
// The Groups list of a talkgroup row: a field the operator reaches by picking in it.
// ---------------------------------------------------------------------------

function groupsMarker(row) {
  return findAll(row, (node) => node.dataset?.invalid === 'true')[0] ?? null;
}

function tickGroup(row, checked) {
  const box = findAll(row, (node) => node.tag === 'input' && node.type === 'checkbox')[0];

  box.checked = checked;
  box.dispatchEvent({ type: 'change' });
  connectFields(row);
}

test('the Groups list of a new talkgroup row is unmarked until the operator picks in it', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-talkgroups'));

  assert.equal(groupsMarker(row), null, 'a list the operator has not reached carries no mark');

  tickGroup(row, true);

  assert.equal(groupsMarker(row), null, 'a group picked out of the list leaves the row valid');

  tickGroup(row, false);

  assert.ok(groupsMarker(row), 'clearing the list marks it, because the row the operator reached needs a group');
});
