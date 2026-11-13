// the Logs filter form's controls: the fields, the names they carry, and the order the page puts them in
// the form's columns may answer the room the card has; the controls those columns lay out do not change

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-logs.js';

import { findAll } from './dom-shim.js';

const FIELD_NAMES = ['Sort order', 'Date', 'Level', 'Subsystem', 'Text', 'Auto-refresh'];
const ACTION_NAMES = ['Reload', 'Reset'];

// The panel asks for entries the moment it opens. This test reads the form, so the answer is an empty
// page and no transport is involved.
function logsForm() {
  const context = getAdminContext();

  context.api.searchLogs = async () => ({ count: 0, logs: [], dateStart: null, dateStop: null });

  const el = new (customElements.get('rdio-admin-logs'))();

  // the panel builds its own markup, and the shim connects every element a browser would as the markup is
  // inserted; the extra pass the tests that build fields by hand need would connect the panel's switch twice
  el.connectedCallback();

  return findAll(el, (node) => node.tag === 'form')[0];
}

// The control the operator reaches and a screen reader names: a pick list's own select, a toggle's own
// button. The host that builds either is not the control, and the name the page gives a control rides on
// that host.
function fieldControl(field) {
  const host = findAll(field, (node) => node.tag.includes('-') && node.getAttribute('name') !== null)[0] ?? null;

  const control = findAll(field, (node) => ['input', 'select', 'textarea'].includes(node.tag))[0]
    ?? findAll(field, (node) => node.tag === 'button')[0];

  return { host, control };
}

function under(node, ancestor) {
  let at = node.parent;

  while (at) {
    if (at === ancestor) return true;

    at = at.parent;
  }

  return false;
}

// A control is named by its own aria-label, or by the field label attached to it. The page writes the
// first on its pick lists and its switch, the primitive wires the second onto every control.
function accessibleName(control, root) {
  const own = control.getAttribute('aria-label');

  if (own) return own;

  const label = findAll(root, (node) => node.tag === 'label').find((node) => node.htmlFor === control.id);

  return label?.textContent ?? null;
}

test('the filter form carries the six fields, in the order the shipped page gives them', () => {
  const form = logsForm();

  assert.ok(form, 'the Logs section renders no filter form to read');

  const fields = findAll(form, (node) => node.tag === 'rdio-form-field');

  assert.deepEqual(fields.map((field) => {
    const { host, control } = fieldControl(field);

    return { name: accessibleName(control, form), control: control.tag, key: host?.getAttribute('name') ?? control.getAttribute('name') };
  }), [
    { name: 'Sort order', control: 'select', key: 'sort' },
    { name: 'Date', control: 'input', key: 'date' },
    { name: 'Level', control: 'select', key: 'level' },
    { name: 'Subsystem', control: 'select', key: 'source' },
    { name: 'Text', control: 'input', key: 'text' },
    { name: 'Auto-refresh', control: 'button', key: 'autoRefresh' },
  ], 'the filter form is the same six fields, named as the operator names them, in the same order');

  assert.equal(findAll(form, (node) => node.tag === 'rdio-form-field').length, FIELD_NAMES.length,
    'the filter form carries no field beside the six it has today');
});

test('the form hosts a toggle for auto-refresh, carrying one switch named once', () => {
  const form = logsForm();

  const hosts = findAll(form, (node) => node.tag === 'rdio-toggle');

  assert.equal(hosts.length, 1, 'the filter form carries exactly one auto-refresh host');

  const host = hosts[0];
  const switches = findAll(host, (node) => node.tag === 'button' && node.getAttribute('role') === 'switch');

  assert.equal(switches.length, 1, 'the auto-refresh host carries the button the operator reaches');

  const control = switches[0];

  assert.ok(control.hasAttribute('aria-checked'), 'the switch does not announce which way it is set');
  assert.equal(control.getAttribute('aria-label'), 'Auto-refresh', 'the switch carries the name the page gives it');
  assert.equal(host.getAttribute('aria-label'), null, 'the name rides on the switch, so the tree carries one name');
  assert.equal(accessibleName(control, form), 'Auto-refresh', 'the switch is named where the role rides');
});

test('the action row holds the two buttons the form answers with, and nothing else', () => {
  const form = logsForm();

  const rows = findAll(form, (node) => node.tag === 'div' && node.className?.split(/\s+/).includes('actions'));

  assert.equal(rows.length, 1, 'the filter form carries one action row');

  const buttons = findAll(rows[0], (node) => node.tag === 'button');

  assert.deepEqual(buttons.map((button) => button.textContent), ACTION_NAMES,
    'the action row answers with Reload and Reset, in that order, and no other control');

  // no control of the form sits outside a field or the action row: the grid lays out both, and a loose
  // control would be laid out on its own
  const owners = findAll(form, (node) => node.tag === 'rdio-form-field'
    || (node.tag === 'div' && node.className?.split(/\s+/).includes('actions')));

  const loose = findAll(form, (node) => ['input', 'select', 'textarea', 'button'].includes(node.tag))
    .filter((node) => !owners.some((owner) => under(node, owner)));

  assert.deepEqual(loose.map((node) => node.tag), [], 'every control of the filter form belongs to a field or the action row');
});
