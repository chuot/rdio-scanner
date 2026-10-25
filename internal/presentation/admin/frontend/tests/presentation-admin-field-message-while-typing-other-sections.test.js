// field messages outside the systems tree answer keystrokes;
// the Tools password panel and the dialog forms have none

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
import '../presentation/admin/rdio-admin-logs.js';
import '../presentation/admin/rdio-systems-selection.js';

import { connectFields, fieldMessage, findAll, keystroke, reachField, texts } from './dom-shim.js';

const METRO = { id: 1, label: 'Metro', order: 2, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };

const GROUPS = [{ id: 1, label: 'Dispatch', alert: '', led: '' }];
const TAGS = [{ id: 3, label: 'Fire', alert: '', led: '' }];

const ACCESS = [{ id: 1, order: 1, code: 'alpha', ident: 'AAA', systems: '*' }];
const APIKEYS = [{ id: 1, order: 1, key: 'first-key', ident: 'K1', systems: '*', disabled: false }];
const DOWNSTREAMS = [{ id: 1, order: 1, url: 'https://first.example', apikey: 'dk-1', systems: '*', disabled: false }];

// a dirwatch row with no mask tokens and its System and Talkgroup selects empty:
// the row the mask rule is read against
const DIRWATCH = [{ id: 1, order: 1, directory: '/var/spool/first', extension: 'mp3', mask: '', type: 'default', systemId: null, siteId: null, talkgroupId: null, frequency: 1, delay: 2000, disabled: false }];

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

function seed(store) {
  store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    alerts: { fire: {} },
    'section:systems': slice([METRO]),
    'section:groups': slice(GROUPS),
    'section:tags': slice(TAGS),
    'section:access': slice(ACCESS),
    'section:apikeys': slice(APIKEYS),
    'section:downstreams': slice(DOWNSTREAMS),
    'section:dirwatch': slice(DIRWATCH),
    'section:options': slice(OPTIONS),
  });

  return store;
}

function attach(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

function addRow(el) {
  findAll(el, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes('add'))[0].click();
  connectFields(el);

  return rows(el)[0];
}

function rows(el) {
  // row locators match collapsible details rows and plain rows alike
  return findAll(el, (node) => node.tag === 'details'
    || (node.tag === 'div' && node.className?.split(/\s+/).includes('rdio-admin-row')));
}

function field(row, title) {
  const fields = findAll(row, (node) => node.tag === 'rdio-form-field');

  // some sections name the field in a label inside it;
  // Groups, Tags, and Options name it in the description row beside it
  return fields.find((node) => texts(findAll(node, (child) => child.tag === 'label'))[0] === title)
    ?? fields.find((node) => findAll(node.parent, (child) => child.className?.split(/\s+/).includes('field-title'))[0]?.textContent === title);
}

function controlOf(row, title) {
  return findAll(field(row, title), (node) => ['input', 'textarea', 'rdio-select'].includes(node.tag))[0];
}

function messageOf(row, title) {
  return fieldMessage(field(row, title));
}

// types a value and stays in the field; the row re-reads each keystroke
function typeIn(row, title, value) {
  keystroke(controlOf(row, title), value);
  connectFields(row);

  return messageOf(row, title);
}

// The operator leaves the field: the reading the field-error work takes.
function leave(row, title) {
  reachField(controlOf(row, title));
  connectFields(row);

  return messageOf(row, title);
}

// Every message the page is showing under a control, in the order the fields are laid out.
function messagesWithText(root) {
  return findAll(root, (node) => node.className?.split(/\s+/).includes('rdio-form-error') && node.textContent)
    .map((node) => node.textContent);
}

// Groups and Tags: the Label is the row's name field

for (const [name, tag] of [['Group', 'rdio-admin-groups'], ['Tag', 'rdio-admin-tags']]) {
  test(`typing the first letters of a valid ${name.toLowerCase()} Label raises no message`, () => {
    seed(getAdminContext().store);

    const row = addRow(attach(tag));

    assert.equal(typeIn(row, 'Label', 'D'), '', 'the first letter of a valid label says nothing');
    assert.equal(typeIn(row, 'Label', 'Dis'), '', 'the label the operator is typing says nothing');
  });

  test(`an emptied ${name.toLowerCase()} Label reports on the keystroke that empties it`, () => {
    seed(getAdminContext().store);

    const row = addRow(attach(tag));

    assert.equal(typeIn(row, 'Label', 'Dispatch'), '', 'a full label says nothing while the operator is in it');
    assert.equal(typeIn(row, 'Label', ''), 'Label is required', 'the keystroke that empties the label reports it');
  });
}

// identifiers, and the whole-list rule, in Access, API keys, and Downstreams

test('typing the first letters of a valid access code raises no message', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-access'));

  assert.equal(typeIn(row, 'Code', 's'), '', 'the first letter of a valid code says nothing');
  assert.equal(typeIn(row, 'Code', 'scram'), '', 'the code the operator is typing says nothing');
});

test('the access code a saved row already has reports on the keystroke that completes the collision', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-access'));

  assert.equal(typeIn(row, 'Code', 'alph'), '', 'one letter short of the collision says nothing');
  assert.equal(typeIn(row, 'Code', 'alpha'), 'Code is already defined', 'the keystroke that completes the collision reports it');
  assert.equal(typeIn(row, 'Code', 'alphb'), '', 'the keystroke that makes the code unique clears it');
});

test('an emptied access code reports on the keystroke that empties it', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-access'));

  assert.equal(typeIn(row, 'Code', 'scramble'), '', 'a full code says nothing while the operator is in it');
  assert.equal(typeIn(row, 'Code', ''), 'Code is required', 'the keystroke that empties the code reports it');
});

test('the access Ident typed letter by letter raises no message and reports on the keystroke that empties it', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-access'));

  assert.equal(typeIn(row, 'Ident', 'A'), '', 'the first letter of a valid Ident says nothing');
  assert.equal(typeIn(row, 'Ident', 'AAAA'), '', 'the Ident the operator is typing says nothing');
  assert.equal(typeIn(row, 'Ident', ''), 'Ident is required', 'the keystroke that empties the Ident reports it');
});

test('the API key typed letter by letter reports the collision on the completing keystroke', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-apikeys'));

  assert.equal(typeIn(row, 'Key', 'first-ke'), '', 'one letter short of the collision says nothing');
  assert.equal(typeIn(row, 'Key', 'first-key'), 'Key is already defined', 'the keystroke that completes the collision reports it');
  assert.equal(typeIn(row, 'Key', 'first-key-2'), '', 'the keystroke that makes the key unique clears it');
});

test('the API key Ident typed letter by letter reports the collision on the completing keystroke', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-apikeys'));

  assert.equal(typeIn(row, 'Ident', 'K'), '', 'the first letter of a valid Ident says nothing');
  assert.equal(typeIn(row, 'Ident', 'K1'), 'Ident is already defined', 'the keystroke that completes the collision reports it');
});

test('the downstream URL typed character by character reports the collision on the completing keystroke', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-downstreams'));

  assert.equal(typeIn(row, 'URL', 'https://first.exampl'), '', 'one character short of the collision says nothing');
  assert.equal(typeIn(row, 'URL', 'https://first.example'), 'URL is already defined', 'the keystroke that completes the collision reports it');
});

test('an emptied downstream API key reports on the keystroke that empties it', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-downstreams'));

  assert.equal(typeIn(row, 'API key', 'dk-2'), '', 'a full API key says nothing while the operator is in it');
  assert.equal(typeIn(row, 'API key', ''), 'API key is required', 'the keystroke that empties the key reports it');
});

// Dirwatch: the directory, the mask tokens, and the row's minimums

test('the dirwatch directory typed character by character reports the collision on the completing keystroke', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-dirwatch'));

  assert.equal(typeIn(row, 'Directory', '/var/spool/firs'), '', 'one character short of the collision says nothing');
  assert.equal(typeIn(row, 'Directory', '/var/spool/first'), 'Directory is already defined', 'the keystroke that completes the collision reports it');
});

test('the dirwatch extension reports the rule it breaks on the keystroke and clears on the keystroke that fixes it', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-dirwatch'));

  assert.equal(typeIn(row, 'Extension', '-'), 'Invalid extension', 'the keystroke that types a character the rule rejects reports it');
  assert.equal(typeIn(row, 'Extension', 'wav'), '', 'the extension the operator is typing says nothing');
});

// a mask with the system token answers the System field; the talkgroup token
// answers the Talkgroup field, on the keystroke that types each token
test('the dirwatch mask tokens move the System and Talkgroup messages on the keystrokes that type them', () => {
  seed(getAdminContext().store);

  const row = rows(attach('rdio-admin-dirwatch'))[0];

  reachField(controlOf(row, 'System'));
  reachField(controlOf(row, 'Talkgroup'));
  connectFields(row);

  assert.equal(messageOf(row, 'System'), 'System is required', 'the empty select the operator left reports');
  assert.equal(messageOf(row, 'Talkgroup'), 'Talkgroup is required', 'the empty select the operator left reports');

  assert.equal(typeIn(row, 'Mask', '#'), 'Invalid mask', 'a token the rule does not know reports');
  assert.equal(messageOf(row, 'System'), 'System is required', 'the System field is untouched by a keystroke in the mask that names nothing');

  assert.equal(typeIn(row, 'Mask', '#SYS'), '', 'the mask the operator is typing is a mask the rule accepts');
  assert.equal(messageOf(row, 'System'), '', 'the system token in the mask answers the System field on the keystroke that types it');
  assert.equal(messageOf(row, 'Talkgroup'), 'Talkgroup is required', 'the talkgroup token is not in the mask yet');

  assert.equal(typeIn(row, 'Mask', '#SYS #TG'), '');
  assert.equal(messageOf(row, 'Talkgroup'), '', 'the talkgroup token in the mask answers the Talkgroup field on the keystroke that types it');
});

test('the dirwatch frequency and delay report their minimums on the keystroke that goes below them', () => {
  seed(getAdminContext().store);

  const row = rows(attach('rdio-admin-dirwatch'))[0];

  assert.equal(typeIn(row, 'Frequency', '0'), 'Invalid frequency', 'the frequency below the floor reports on the keystroke');
  assert.equal(typeIn(row, 'Frequency', '12'), '', 'the frequency the operator is typing says nothing');

  assert.equal(typeIn(row, 'Delay', '1'), 'Delay value cannot be less than 2000 milliseconds.', 'the delay below the floor reports on the keystroke');
  assert.equal(typeIn(row, 'Delay', '2000'), '', 'the delay the operator typed reports nothing once it reaches the floor');
});

// Options: one form, no rows

function optionsField(el, key) {
  const row = findAll(el, (node) => node.getAttribute?.('data-control') === key)[0];

  return findAll(row, (node) => node.tag === 'rdio-form-field')[0];
}

function typeInOption(el, key, value) {
  keystroke(findAll(optionsField(el, key), (node) => ['input', 'rdio-select'].includes(node.tag))[0], value);
  connectFields(el);

  return fieldMessage(optionsField(el, key));
}

test('Max Clients answers the number being typed: a valid value, a value below the minimum, an emptied field', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-options');

  assert.equal(typeInOption(el, 'maxClients', '1'), '', 'the minimum the rule accepts says nothing');
  assert.equal(typeInOption(el, 'maxClients', '0'), 'Max clients is invalid', 'the keystroke that goes below the minimum reports it');
  assert.equal(typeInOption(el, 'maxClients', ''), 'Max clients is required', 'the keystroke that empties the field reports it');
});

test('Prune Days answers the number being typed, and zero is a value the rule accepts', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-options');

  assert.equal(typeInOption(el, 'pruneDays', '0'), '', 'disabling the prune is a value the rule accepts');
  assert.equal(typeInOption(el, 'pruneDays', ''), 'Prune days is required', 'the keystroke that empties the field reports it');
});

// the whole-application sweep: the Tools password panel answers keystrokes,
// while the file forms, Logs filters, and systems dialog carry no field messages

function toolsPanel() {
  seed(getAdminContext().store);

  return attach('rdio-admin-tools');
}

test('the Change password panel reports a short password on the keystroke and clears it at eight characters', () => {
  const el = toolsPanel();

  const input = findAll(el, (node) => node.attributes.name === 'newPassword')[0];
  const newPassword = input.parent;

  keystroke(input, 'abc');

  assert.equal(fieldMessage(newPassword), 'Password is too short', 'the keystroke that leaves the password short reports it');

  keystroke(input, 'abcdefgh');

  assert.equal(fieldMessage(newPassword), '', 'the keystroke that reaches eight characters clears it, with the operator still in the field');
});

test('the Tools file forms carry no field message while the operator reaches them', () => {
  const el = toolsPanel();

  const files = findAll(el, (node) => node.tag === 'input' && node.type === 'file');

  assert.equal(files.length, 3, 'Tools does not carry the file controls of its import and its two CSV forms');

  for (const input of files) {
    input.dispatchEvent({ type: 'input' });
    input.dispatchEvent({ type: 'change' });
  }

  connectFields(el);

  assert.deepEqual(messagesWithText(el), [], 'no field in the Tools file forms says anything while the operator is in it');
});

// the Logs panel requests entries on open; the test stubs an empty page
// so the reading involves no transport
function logsPanel() {
  const context = getAdminContext();

  context.api.searchLogs = async () => ({ count: 0, logs: [], dateStart: null, dateStop: null });

  seed(context.store);

  return attach('rdio-admin-logs');
}

test('the Logs filter fields carry no message while the operator types in them and leaves them', async () => {
  const el = logsPanel();

  const text = findAll(el, (node) => node.tag === 'input' && node.attributes.name === 'text')[0];
  const date = findAll(el, (node) => node.tag === 'input' && node.attributes.name === 'date')[0];

  keystroke(text, 'no such call');
  keystroke(date, '2026-10-13T08:00');
  reachField(text);
  connectFields(el);

  // The panel's opening request settles on the microtask queue; the reading is taken once it has.
  await new Promise((resolve) => setImmediate(resolve));

  assert.deepEqual(messagesWithText(el), [], 'the filters say nothing about what the operator types in them');
});

test('the systems-selection dialog carries no field message while the operator ticks it', () => {
  const tree = new (customElements.get('rdio-systems-selection'))();

  tree.setup({
    groups: GROUPS,
    tags: TAGS,
    systems: [{ systemRef: 1, label: 'Metro', talkgroups: [{ talkgroupRef: 70, label: 'Fire', groupIds: [1], tagId: 3 }] }],
  }, '*');

  const boxes = findAll(tree, (node) => node.tag === 'input' && node.type === 'checkbox');

  assert.ok(boxes.length >= 4, 'the dialog lists fewer boxes than its tree of groups, tags, systems, and talkgroups');

  for (const box of boxes) {
    box.checked = true;
    box.dispatchEvent({ type: 'change' });
  }

  connectFields(tree);

  assert.deepEqual(messagesWithText(tree), [], 'the dialog says nothing while the operator is ticking it');
});

// the leaving readings still match the field-error tests: same fields, one rule

test('the message a keystroke raises is the message the field carries when the operator leaves it', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-access'));

  typeIn(row, 'Code', 'alpha');

  assert.equal(leave(row, 'Code'), 'Code is already defined', 'the leaving answers the same rule the keystroke answered');
});
