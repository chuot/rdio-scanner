// The seven sections outside the systems tree, plus the password panel.
// A hand-built section names its field by a label inside the field or a caption span.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-groups.js';
import '../presentation/admin/rdio-admin-tags.js';
import '../presentation/admin/rdio-admin-access.js';
import '../presentation/admin/rdio-admin-apikeys.js';
import '../presentation/admin/rdio-admin-downstreams.js';
import '../presentation/admin/rdio-admin-dirwatch.js';
import '../presentation/admin/rdio-admin-options.js';
import '../presentation/admin/rdio-admin-tools.js';

import { connectFields, fieldMessage, findAll, reachField, requiredMark, texts } from './dom-shim.js';

const METRO = { id: 1, label: 'Metro', order: 2, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };
const COUNTY = { id: 2, label: 'County', order: 1, systemRef: 2, blacklists: '', sites: [], talkgroups: [], units: [] };

const GROUPS = [{ id: 1, label: 'Dispatch', alert: '', led: '' }];
const TAGS = [{ id: 3, label: 'Fire', alert: '', led: '' }];

const ACCESS = [{ id: 1, order: 1, code: 'alpha', ident: 'AAA', systems: '*' }];
const APIKEYS = [{ id: 1, order: 1, key: 'first-key', ident: 'K1', systems: '*', disabled: false }];
const DOWNSTREAMS = [{ id: 1, order: 1, url: 'https://first.example', apikey: 'dk-1', systems: '*', disabled: false }];
const DIRWATCH = [{ id: 1, order: 1, directory: '/var/spool/first', extension: 'mp3', mask: '#SYS #TG', type: 'p25', systemId: 2, siteId: null, talkgroupId: 7, frequency: 1, delay: 2000, disabled: false }];

const OPTIONS = {
  audioConversion: 'opus',
  autoPopulate: false,
  branding: 'Ridio',
  disableDuplicateDetection: 0,
  email: 'ops@example.test',
  keypadBeeps: 'standard',
  maxClients: 5,
  pruneDays: 7,
  showListenersCount: 0,
  sortTalkgroups: false,
};

function slice(working) {
  return { baseline: structuredClone(working), working: structuredClone(working), dirty: false, invalid: false, conflict: false };
}

function seed(store, extra = {}) {
  store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    alerts: { fire: {} },
    'section:systems': slice([METRO, COUNTY]),
    'section:groups': slice(GROUPS),
    'section:tags': slice(TAGS),
    'section:access': slice(ACCESS),
    'section:apikeys': slice(APIKEYS),
    'section:downstreams': slice(DOWNSTREAMS),
    'section:dirwatch': slice(DIRWATCH),
    'section:options': slice(OPTIONS),
    ...extra,
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

function rows(el) {
  // The row locators find collapsible details rows and plain rows alike.
  return findAll(el, (node) => node.tag === 'details'
    || (node.tag === 'div' && node.className?.split(/\s+/).includes('rdio-admin-row')));
}

// The Tools panels are `details` elements carrying the same row class the config rows use.
function panels(el) {
  return findAll(el, (node) => node.className?.split(/\s+/).includes('rdio-admin-row'));
}

function addRow(el) {
  control(el, 'add').click();
  connectFields(el);

  return rows(el)[0];
}

// The names a row carries the asterisk on; a name counts once.
function markedNames(row) {
  const names = new Set();

  for (const node of findAll(row, (child) => child.tag === 'rdio-form-field')) {
    const label = findAll(node, (child) => child.tag === 'label')[0];

    if (label && requiredMark(label)) names.add(label.textContent);
  }

  for (const span of findAll(row, (child) => ['field-title', 'body'].includes(spanClass(child)))) {
    if (requiredMark(span)) names.add(span.textContent);
  }

  return [...names].sort();
}

function spanClass(node) {
  return node.className?.split(/\s+/).find((name) => name === 'field-title' || name === 'body');
}

function fieldOf(row, title) {
  // The label and the caption both live inside the form field.
  return findAll(row, (node) => node.tag === 'rdio-form-field')
    .find((node) => findAll(node, (child) => child.tag === 'label' || spanClass(child))
      .some((el) => el.textContent === title));
}

// The control the operator types in or picks in, under the name the row gives it.
function controlOf(row, title) {
  return findAll(fieldOf(row, title), (node) => ['input', 'textarea', 'rdio-select'].includes(node.tag))[0];
}

// The operator types in the field and the change fires the way leaving the field fires it.
function typeIn(row, title, value) {
  const control = controlOf(row, title);

  control.value = value;
  control.dispatchEvent({ type: 'change' });
  connectFields(row);

  return control;
}

// ---------------------------------------------------------------------------
// The rows the operator added, read before they reach anything.
// ---------------------------------------------------------------------------

const MARKED_ROWS = [
  ['groups', 'rdio-admin-groups', ['Label']],
  ['tags', 'rdio-admin-tags', ['Label']],
  ['access', 'rdio-admin-access', ['Code', 'Ident']],
  ['API keys', 'rdio-admin-apikeys', ['Ident', 'Key']],
  ['downstreams', 'rdio-admin-downstreams', ['API key', 'URL']],
  ['dirwatch', 'rdio-admin-dirwatch', ['Directory']],
];

for (const [name, tag, marked] of MARKED_ROWS) {
  test(`a new ${name} row names the fields the webapp insists on`, () => {
    seed(getAdminContext().store);

    assert.deepEqual(markedNames(addRow(attach(tag))), marked, `the new ${name} row carries the mark on its name and nowhere else`);
  });
}

// ---------------------------------------------------------------------------
// The Options panel is one form, not a list of rows: the marks are read on the panel as it renders.
// ---------------------------------------------------------------------------

test('the options panel names the fields the webapp insists on', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-options');

  assert.deepEqual(
    markedNames(el),
    ['Keypad Beep Style', 'Max Clients', 'Prune Days'],
    'the three fields the options rules require carry the mark on their name',
  );
});

test('the options panel leaves the fields it accepts empty plain', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-options');

  for (const title of ['Audio Conversion', 'Auto Populate', 'Branding Label', 'Disable Duplicate Call Detection', 'Email Support', 'Show Listeners Count', 'Sort Talkgroups']) {
    assert.ok(!markedNames(el).includes(title), `${title} is not insisted on and carries no mark`);
  }
});

// ---------------------------------------------------------------------------
// Tools → Change password: the panel where Material showed its asterisks.
// ---------------------------------------------------------------------------

test('the change password panel names the fields the webapp insists on', () => {
  seed(getAdminContext().store);

  const tools = attach('rdio-admin-tools');
  const panel = panels(tools).find((row) => texts(findAll(row, (child) => child.className?.split(/\s+/).includes('row-title')))[0] === 'Change password');

  assert.deepEqual(
    markedNames(panel),
    ['Current password', 'New password', 'Verify new password'],
    'the three password boxes carry the mark on their name, as Material showed them',
  );
});

// The conditional requirements: System and Talkgroup stay plain, their rules report.
// Only a saved row whose mask names neither id can put the two selects in view.

test('the dirwatch row leaves System and Talkgroup plain while their rules report them', () => {
  seed(getAdminContext().store, {
    'section:dirwatch': slice([{ id: 1, order: 1, directory: '/var/spool/first', extension: 'mp3', mask: '', type: 'default', systemId: null, siteId: null, talkgroupId: null, frequency: 1, delay: 2000, disabled: false }]),
  });

  const row = rows(attach('rdio-admin-dirwatch'))[0];

  // The operator reaches both empty selects: both rules report, no mark.
  reachField(controlOf(row, 'System'));
  reachField(controlOf(row, 'Talkgroup'));
  connectFields(row);

  assert.equal(fieldMessage(fieldOf(row, 'System')), 'System is required', 'the empty select the operator left reports');
  assert.equal(fieldMessage(fieldOf(row, 'Talkgroup')), 'Talkgroup is required', 'the empty select the operator left reports');
  assert.ok(!markedNames(row).includes('System') && !markedNames(row).includes('Talkgroup'), 'the two fields the row asks for in this state carry no mark');

  // The mask token answers the System rule: the report goes away, and the name stays plain.
  typeIn(row, 'Mask', '#SYS');

  assert.equal(fieldMessage(fieldOf(row, 'System')), '', 'the system token in the mask answers the System field');
  assert.equal(fieldMessage(fieldOf(row, 'Talkgroup')), 'Talkgroup is required', 'the talkgroup token is not in the mask yet');
  assert.ok(!markedNames(row).includes('System') && !markedNames(row).includes('Talkgroup'), 'the marks do not arrive with the state that makes the rule bite');
});

// ---------------------------------------------------------------------------
// A saved row carries the same marks as a new one.
// ---------------------------------------------------------------------------

test('a saved access row carries the same marks as the row the operator added', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-access');
  const added = addRow(el);
  const saved = rows(el).find((row) => row !== added);

  assert.deepEqual(markedNames(saved), ['Code', 'Ident'], 'the marks answer the fields, not the values in them');
});
