// The seven administration sections the operator named beyond the systems tree — Groups, Tags,
// Access, API keys, Downstreams, Dirwatch, Options — read the way the talkgroups row is read: what a
// row the operator just added says, what appears when they change a field, what the field says after
// they leave a good value, and what is left after the section is discarded.
//
// The readings are taken from the text under the control, written by the form field primitive
// itself. The rows are the rendered rows; the store's working copy is not the screen.

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

import { connectFields, fieldMessage, findAll, reachField, texts } from './dom-shim.js';

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

function addRow(el) {
  control(el, 'add').click();
  connectFields(el);

  return rows(el)[0];
}

function rows(el) {
  return findAll(el, (node) => node.tag === 'div' && node.className?.split(/\s+/).includes('rdio-admin-row'));
}

function field(row, title) {
  const fields = findAll(row, (node) => node.tag === 'rdio-form-field');

  // The systems-tree sections and the row sections with a dialog put a <label> inside the field;
  // Groups, Tags, and Options name the field in the description row beside it.
  return fields.find((node) => texts(findAll(node, (child) => child.tag === 'label'))[0] === title)
    ?? fields.find((node) => findAll(node.parent, (child) => child.className?.split(/\s+/).includes('field-title'))[0]?.textContent === title);
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

function barButton(el, className) {
  const bar = findAll(el, (node) => node.tag === 'rdio-admin-section-bar')[0];

  bar.connectedCallback();

  return findAll(bar, (node) => node.tag === 'button' && node.className.split(/\s+/).includes(className))[0];
}

// ---------------------------------------------------------------------------
// The rows the operator adds in the six row sections.
// ---------------------------------------------------------------------------

const ROW_SECTIONS = [
  ['Groups', 'rdio-admin-groups', 'New Group', 'Label', 'Dispatch'],
  ['Tags', 'rdio-admin-tags', 'New Tag', 'Label', 'Fire'],
  ['Access', 'rdio-admin-access', 'New access', 'Code', 'scramble'],
  ['API keys', 'rdio-admin-apikeys', 'New API key', 'Key', 'a-key'],
  ['Downstreams', 'rdio-admin-downstreams', 'New downstream', 'URL', 'https://second.example'],
  ['Dirwatch', 'rdio-admin-dirwatch', 'New dir watch', 'Directory', '/var/spool/second'],
];

for (const [name, tag, addLabel, title, goodValue] of ROW_SECTIONS) {
  test(`a new ${name.toLowerCase()} row says nothing about fields the operator has not reached`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);
    const row = addRow(el);

    assert.deepEqual(messages(row), [], `the row the ${addLabel} control just created is silent`);
  });

  test(`changing a field of a new ${name.toLowerCase()} row reveals its message, and a good value clears it`, () => {
    seed(getAdminContext().store);

    const row = addRow(attach(tag));

    typeIn(row, title, '');

    assert.ok(fieldMessage(field(row, title)), `the field the operator cleared carries its message`);

    typeIn(row, title, goodValue);

    assert.equal(fieldMessage(field(row, title)), '', 'the good value they left clears the message');

    typeIn(row, title, '');

    assert.ok(fieldMessage(field(row, title)), 'clearing the field brings the message back');
  });

  test(`the message a ${name.toLowerCase()} field carries survives the rows being added above it`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);

    // The operator edits a saved row: the section is dirty and the field they edited carries its
    // message. Adding a row is the rebuild the state has to survive.
    typeIn(rows(el)[0], title, '');

    assert.ok(fieldMessage(field(rows(el)[0], title)), 'the edited field carries its message');

    addRow(el);

    assert.ok(fieldMessage(field(rows(el)[1], title)), 'the row moved under the new one keeps the message on the field it belongs to');
  });

  test(`discarding the ${name.toLowerCase()} section leaves every field unreached`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);

    typeIn(rows(el)[0], title, '');

    assert.ok(fieldMessage(field(rows(el)[0], title)), 'the edited field carries its message');

    barButton(el, 'discard').click();

    assert.deepEqual(messages(rows(el)[0]), [], 'the discarded section says nothing about a field the operator has not reached again');
  });
}

// ---------------------------------------------------------------------------
// The rules a row shares with the rows beside it: an identifier another row has.
// ---------------------------------------------------------------------------

const DUPLICATES = [
  ['Access', 'rdio-admin-access', 'Code', 'alpha', 'Code is already defined'],
  ['API keys', 'rdio-admin-apikeys', 'Key', 'first-key', 'Key is already defined'],
  ['Downstreams', 'rdio-admin-downstreams', 'URL', 'https://first.example', 'URL is already defined'],
  ['Dirwatch', 'rdio-admin-dirwatch', 'Directory', '/var/spool/first', 'Directory is already defined'],
];

for (const [name, tag, title, taken, message] of DUPLICATES) {
  test(`a ${name.toLowerCase()} row that takes an identifier another row has reports it on the field carrying it`, () => {
    seed(getAdminContext().store);

    const row = addRow(attach(tag));

    typeIn(row, title, taken);

    assert.equal(fieldMessage(field(row, title)), message);
  });
}

// ---------------------------------------------------------------------------
// The Options section: one form, no rows.
// ---------------------------------------------------------------------------

function optionsField(el, key) {
  const row = findAll(el, (node) => node.getAttribute?.('data-control') === key)[0];

  return findAll(row, (node) => node.tag === 'rdio-form-field')[0];
}

function optionsMessage(el, key) {
  return fieldMessage(optionsField(el, key));
}

function typeInOption(el, key, value) {
  const control = findAll(optionsField(el, key), (node) => ['input', 'rdio-select'].includes(node.tag))[0];

  control.value = value;
  control.dispatchEvent({ type: 'change' });
  connectFields(el);

  return control;
}

test('the Options section opens with every field silent', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-options');

  for (const key of ['maxClients', 'pruneDays', 'email', 'keypadBeeps']) {
    assert.equal(optionsMessage(el, key), '', `${key} says nothing to an operator who has not reached it`);
  }
});

test('clearing Max Clients reveals its message and a good value clears it', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-options');

  typeInOption(el, 'maxClients', '');

  assert.equal(optionsMessage(el, 'maxClients'), 'Max clients is required');

  typeInOption(el, 'maxClients', '5');

  assert.equal(optionsMessage(el, 'maxClients'), '');
});

test('discarding Options leaves every field unreached', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-options');

  typeInOption(el, 'maxClients', '');

  assert.equal(optionsMessage(el, 'maxClients'), 'Max clients is required');

  barButton(el, 'discard').click();

  assert.equal(optionsMessage(el, 'maxClients'), '');
});
