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

function slice(baseline) {
  return { baseline, working: structuredClone(baseline), dirty: false, invalid: false, conflict: false };
}

function seed(store) {
  store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    alerts: {},
    'section:systems': slice([METRO]),
    'section:groups': slice(GROUPS),
    'section:tags': slice(TAGS),
    'subsection:talkgroups:1': slice(TALKGROUPS[1]),
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
