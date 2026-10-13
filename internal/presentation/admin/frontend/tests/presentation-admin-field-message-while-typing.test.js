// The message under a field, read while the operator is typing in it.
//
// The form field reveals a message the moment the value in the control changes, and the section
// recomputes that message from the value it holds. The readings here stop before the operator leaves
// the field, which is the state the page is in while the operator is in it: the message must answer
// the value in the control, not the value the section last committed.
//
// The leaving is read with reachField, the way the field-error work reads it, so the two rules are
// read against the same field.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-talkgroups.js';

import { connectFields, fieldMessage, findAll, keystroke, reachField } from './dom-shim.js';

const METRO = { id: 1, label: 'Metro', order: 1, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };

const GROUPS = [{ id: 1, label: 'Dispatch' }];
const TAGS = [{ id: 3, label: 'Fire' }];

const TALKGROUPS = {
  1: [{ id: 7, label: 'Fire', name: 'Fire dispatch', talkgroupRef: 70, order: 1, groupIds: [1], tagId: 3, alert: '', delay: null, frequency: null, led: '', type: '' }],
};

// Two saved talkgroups: the pair the duplicate identifier is read against.
const PAIR = [
  { id: 7, label: 'Fire', name: 'Fire dispatch', talkgroupRef: 70, order: 1, groupIds: [1], tagId: 3, alert: '', delay: null, frequency: null, led: '', type: '' },
  { id: 8, label: 'Talk', name: 'Talk dispatch', talkgroupRef: 90, order: 2, groupIds: [1], tagId: 3, alert: '', delay: null, frequency: null, led: '', type: '' },
];

// A saved row the operator came back to finish: the Label and the Name are what it is missing.
const UNFINISHED = [{ id: 7, label: '', name: '', talkgroupRef: 70, order: 1, groupIds: [1], tagId: 3, alert: '', delay: null, frequency: null, led: '', type: '' }];

function slice(baseline) {
  return { baseline, working: structuredClone(baseline), dirty: false, invalid: false, conflict: false };
}

function seed(store, talkgroups = TALKGROUPS[1]) {
  store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    alerts: {},
    'section:systems': slice([METRO]),
    'section:groups': slice(GROUPS),
    'section:tags': slice(TAGS),
    'subsection:talkgroups:1': slice(talkgroups),
  });

  return store;
}

function attach(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

// The section on the page, with a brand-new row at the top of its list.
function sectionWithNewRow(tag) {
  const el = attach(tag);

  findAll(el, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes('add'))[0].click();
  connectFields(el);

  return { el, row: rows(el)[0] };
}

function rows(el) {
  return findAll(el, (node) => node.tag === 'details');
}

function field(row, title) {
  return findAll(row, (node) => node.tag === 'rdio-form-field')
    .find((node) => findAll(node, (child) => child.tag === 'label')[0]?.textContent === title);
}

function controlOf(row, title) {
  return findAll(field(row, title), (node) => ['input', 'textarea', 'rdio-select'].includes(node.tag))[0];
}

function messageOf(row, title) {
  return fieldMessage(field(row, title));
}

// The operator types a value into the field and stays in it. The row is re-read from the page after
// every keystroke, the way it is on screen.
function typeIn(row, title, value) {
  keystroke(controlOf(row, title), value);
  connectFields(row);

  return messageOf(row, title);
}

function leave(row, title) {
  reachField(controlOf(row, title));
  connectFields(row);

  return messageOf(row, title);
}

// The warning icon the row is showing beside its title.
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

// The section's working copy: what the page holds between the keystrokes and a save.
function workingRows() {
  return getAdminContext().store.getState()['subsection:talkgroups:1'].working;
}

// ---------------------------------------------------------------------------
// The keystroke the operator is typing: the message answers the value in the control.
// ---------------------------------------------------------------------------

test('typing the first digit of a valid Id raises no message', () => {
  seed(getAdminContext().store);

  const { row } = sectionWithNewRow('rdio-admin-talkgroups');

  assert.equal(typeIn(row, 'Id', '1'), '', 'the first digit of a valid talkgroup identifier says nothing');
});

test('typing the first letter of a valid Label raises no message', () => {
  seed(getAdminContext().store);

  const { row } = sectionWithNewRow('rdio-admin-talkgroups');

  assert.equal(typeIn(row, 'Label', 'F'), '', 'the first letter of a valid label says nothing');
});

test('a message the operator is correcting goes away on the keystroke that fixes it', () => {
  seed(getAdminContext().store);

  const { row } = sectionWithNewRow('rdio-admin-talkgroups');

  typeIn(row, 'Id', '0');

  assert.equal(leave(row, 'Id'), 'Id is invalid', 'the invalid identifier reports when the operator leaves it');

  assert.equal(typeIn(row, 'Id', '1'), '', 'the valid identifier the operator is typing says nothing');
});

test('the keystroke that breaks a rule reports it without waiting for the operator to leave', () => {
  seed(getAdminContext().store);

  const { row } = sectionWithNewRow('rdio-admin-talkgroups');

  assert.equal(typeIn(row, 'Id', '0'), 'Id is invalid', 'the identifier below the minimum reports on the keystroke');
});

// ---------------------------------------------------------------------------
// The rule about the whole list: an identifier another talkgroup of the system has.
// ---------------------------------------------------------------------------

test('a talkgroup Id typed digit by digit reports the duplicate on the keystroke that completes the collision', () => {
  seed(getAdminContext().store, PAIR);

  const row = rows(attach('rdio-admin-talkgroups'))[0];

  assert.equal(typeIn(row, 'Id', '9'), '', 'one digit short of the collision says nothing');
  assert.equal(marker(row), null, 'the row carries no warning icon while its identifier is its own');

  assert.equal(typeIn(row, 'Id', '90'), 'Id is already defined', 'the keystroke that completes the collision reports it');
  assert.ok(marker(row), 'the row carries its warning icon on the keystroke that collides');
});

test('the duplicate a talkgroup Id carries goes away on the keystroke that makes it unique', () => {
  seed(getAdminContext().store, PAIR);

  const row = rows(attach('rdio-admin-talkgroups'))[0];

  typeIn(row, 'Id', '90');

  assert.equal(messageOf(row, 'Id'), 'Id is already defined', 'the collision is reported while the operator is in the field');

  assert.equal(typeIn(row, 'Id', '91'), '', 'the keystroke that makes the identifier unique clears it');
  assert.equal(marker(row), null, 'the row loses its warning icon on the same keystroke');
});

// ---------------------------------------------------------------------------
// The row under the operator's hands: the same field, the same row, every keystroke.
// ---------------------------------------------------------------------------

test('a run of keystrokes into a talkgroup Label keeps the field on the page and the value the operator typed', () => {
  seed(getAdminContext().store, PAIR);

  const row = rows(attach('rdio-admin-talkgroups'))[0];
  const labelField = field(row, 'Label');

  for (const typed of ['T', 'Ta', 'Tac', 'Tact']) {
    assert.equal(typeIn(row, 'Label', typed), '', `the label ${typed} says nothing while the operator is typing it`);
    assert.equal(field(row, 'Label'), labelField, 'the field the operator is typing in is the same field on the page');
    assert.equal(workingRows()[0].label, typed, 'the section holds the value the operator typed, not the one it last committed on a leaving');
  }
});

test('the message under the field answers the value typed at every keystroke of the run', () => {
  seed(getAdminContext().store, PAIR);

  const row = rows(attach('rdio-admin-talkgroups'))[0];
  const idField = field(row, 'Id');
  const readings = [];

  for (const typed of ['9', '90', '900']) {
    readings.push(typeIn(row, 'Id', typed));
    assert.equal(field(row, 'Id'), idField, 'the field is not replaced under the operator caret');
    assert.equal(workingRows()[0].talkgroupRef, Number(typed), 'the section holds the identifier being typed');
  }

  assert.deepEqual(readings, ['', 'Id is already defined', ''], 'the message follows the value in the control through the run');
});

// ---------------------------------------------------------------------------
// The marks beside the field: the row's warning icon and the section's Save control.
// ---------------------------------------------------------------------------

test('the first keystroke into a valid talkgroup Id lights the Save control', () => {
  seed(getAdminContext().store, PAIR);

  const el = attach('rdio-admin-talkgroups');
  const row = rows(el)[0];

  assert.equal(saveButton(el).disabled, true, 'a section the operator has not touched cannot be saved');

  assert.equal(typeIn(row, 'Id', '1'), '', 'the identifier the operator is typing is a valid one');
  assert.equal(saveButton(el).disabled, false, 'the first keystroke lights the Save control');
  assert.equal(marker(row), null, 'the valid row carries no warning icon');
});

test('the row the operator is filling keeps its warning icon until its last field is filled, and takes it back with the message', () => {
  seed(getAdminContext().store, UNFINISHED);

  const el = attach('rdio-admin-talkgroups');
  const row = rows(el)[0];

  assert.ok(marker(row), 'the row the operator came back to finish carries its warning icon');
  assert.equal(saveButton(el).disabled, true, 'the Save control stays disabled while the section carries an error');

  assert.equal(typeIn(row, 'Id', '1'), '', 'the identifier the operator types is a valid one');
  assert.ok(marker(row), 'the row keeps its warning icon while the rest of it is invalid');
  assert.equal(saveButton(el).disabled, true, 'the Save control stays disabled while the section carries the row being filled');

  assert.equal(typeIn(row, 'Label', 'Fire'), '');
  assert.equal(typeIn(row, 'Name', 'Fire dispatch'), '');
  assert.equal(marker(row), null, 'filling the row takes its warning icon away on the keystroke that fills it');
  assert.equal(saveButton(el).disabled, false, 'the Save control answers the section the keystrokes made valid');

  assert.equal(typeIn(row, 'Id', ''), 'Id is required', 'the keystroke that empties the identifier reports it');
  assert.ok(marker(row), 'and the row takes its warning icon back on the same keystroke');
});
