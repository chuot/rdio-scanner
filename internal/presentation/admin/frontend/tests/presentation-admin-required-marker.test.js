// The names the administration webapp marks with an asterisk, in the four sections built by the
// shared row builders: the systems section and the three per-level sections.
//
// The reading is the page, not the section's declaration. A row the operator just added carries the
// mark on the name of every field the webapp insists on, before the operator reaches any field — the
// mark names the rule, not the value in the control, and it is there from the first render.
//
// The glyph is the stylesheet's. The suites read the mark on the naming element, and the browser
// harness reads the character the page renders.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-systems.js';
import '../presentation/admin/rdio-admin-sites.js';
import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-units.js';

import { connectFields, fieldMessage, findAll, requiredMark, texts } from './dom-shim.js';

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

function rows(el) {
  return findAll(el, (node) => node.tag === 'details');
}

// The names a row carries the asterisk on. A field is named by the label inside its form field; a
// hand-built list — the talkgroups Groups boxes — is named by the caption span above it, the shape
// the sections that build their fields by hand use.
function markedNames(row) {
  const names = [];

  for (const node of findAll(row, (child) => child.tag === 'rdio-form-field')) {
    const label = findAll(node, (child) => child.tag === 'label')[0];

    if (label && requiredMark(label)) names.push(label.textContent);
  }

  for (const span of findAll(row, (child) => child.className?.split(/\s+/).includes('body'))) {
    if (requiredMark(span)) names.push(span.textContent);
  }

  return names.sort();
}

// The names a row shows at all, marked or not: the reading is the pair — which names carry the mark,
// and which names the row carries. A field built by the shared row builders is named twice on the
// page, once by the caption block and once by the label inside its form field, so a name counts once.
function shownNames(row) {
  const names = new Set();

  for (const node of findAll(row, (child) => child.tag === 'rdio-form-field')) {
    const label = findAll(node, (child) => child.tag === 'label')[0];

    if (label) names.add(label.textContent);
  }

  for (const span of findAll(row, (child) => child.className?.split(/\s+/).includes('body'))) {
    names.add(span.textContent);
  }

  return [...names].sort();
}

// The caption span a row names its field with: the systems tree writes it as `.body`, the hand-built
// sections as `.field-title`. A label inside a form field is the other shape, and is read by its tag.
function spanClass(node) {
  return node.className?.split(/\s+/).find((name) => name === 'field-title' || name === 'body');
}

function fieldOf(row, title) {
  // The label inside the field, and the caption beside the control: both live inside the form field,
  // so the field is the one whose own text names the title.
  return findAll(row, (node) => node.tag === 'rdio-form-field')
    .find((node) => findAll(node, (child) => child.tag === 'label' || spanClass(child))
      .some((el) => el.textContent === title));
}

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

// The names a row carries, marked or not, minus the marked ones: the fields the webapp accepts empty.
function unmarkedNames(row) {
  const marked = new Set(markedNames(row));

  return shownNames(row).filter((name) => !marked.has(name));
}

// ---------------------------------------------------------------------------
// The fields the webapp accepts empty carry no mark, in the sections built by the row builders.
// ---------------------------------------------------------------------------

const UNMARKED = [
  ['rdio-admin-systems', ['Audio alert', 'Blacklists', 'Delay', 'Led Color', 'Type']],
  ['rdio-admin-sites', []],
  ['rdio-admin-talkgroups', ['Audio alert', 'Delay', 'Frequency', 'Led Color', 'Type']],
  ['rdio-admin-units', ['Id', 'Range from', 'Range to']],
];

for (const [tag, expected] of UNMARKED) {
  test(`a new ${tag} row leaves the fields the webapp accepts empty unmarked`, () => {
    seed(getAdminContext().store);

    const row = addRow(attach(tag));

    for (const name of expected) {
      assert.ok(unmarkedNames(row).includes(name), `${name} is not insisted on and carries no mark`);
    }
  });
}

// ---------------------------------------------------------------------------
// The mark names the rule, not the value: it does not move through valid, invalid, and empty.
// ---------------------------------------------------------------------------

test('the mark on an identifier stays while the row is filled in, broken, and emptied', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-talkgroups');
  const row = addRow(el);

  assert.deepEqual(markedNames(row).includes('Id'), true, 'the new row marks the identifier');

  typeIn(row, 'Id', '71');

  assert.deepEqual(markedNames(row), ['Groups', 'Id', 'Label', 'Name', 'Tag'], 'a valid identifier keeps every mark');

  // The identifier a saved row already has: the whole-list rule names itself on the row.
  typeIn(row, 'Id', '70');

  assert.deepEqual(markedNames(row), ['Groups', 'Id', 'Label', 'Name', 'Tag'], 'a duplicate identifier keeps every mark');

  typeIn(row, 'Id', '');

  assert.deepEqual(markedNames(row), ['Groups', 'Id', 'Label', 'Name', 'Tag'], 'an emptied identifier keeps its mark: the mark names the rule, not the value');
});

test('the mark on a label does not move when the field is filled and broken', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-sites');
  const row = addRow(el);

  typeIn(row, 'Label', 'Depot');

  assert.deepEqual(markedNames(row), ['Id', 'Label'], 'a valid label keeps both marks');

  typeIn(row, 'Label', '');

  assert.deepEqual(markedNames(row), ['Id', 'Label'], 'an emptied label keeps both marks');
});

// ---------------------------------------------------------------------------
// The conditional requirement: the name stays plain, the rule reports.
// ---------------------------------------------------------------------------

test('the unit row leaves its identifier plain while the rule reports it', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-units'));

  // No identifier and no range: the rule insists, and the name carries no mark.
  typeIn(row, 'Id', '');

  assert.equal(fieldMessage(fieldOf(row, 'Id')), 'Id value is required', 'the rule reports the field its name does not mark');
  assert.ok(!markedNames(row).includes('Id'), 'the identifier is required only in this state, so its name stays plain');

  // The identifier the operator types in answers its own rule: the report goes away, the name stays plain.
  typeIn(row, 'Id', '12');

  assert.equal(fieldMessage(fieldOf(row, 'Id')), '', 'the identifier the operator typed says nothing');
  assert.deepEqual(markedNames(row), ['Label'], 'the marks name the label and nothing else, in this state too');
});

// ---------------------------------------------------------------------------
// The row the operator just added, read before they reach anything.
// ---------------------------------------------------------------------------

test('a new system row names the fields the webapp insists on', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-systems'));

  assert.deepEqual(markedNames(row), ['Id', 'Label'], 'the new row marks its identifier and its label');
});

test('a new site row names the fields the webapp insists on', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-sites'));

  assert.deepEqual(markedNames(row), ['Id', 'Label'], 'the new row marks its identifier and its label');
});

test('a new talkgroup row names the fields the webapp insists on', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-talkgroups'));

  assert.deepEqual(
    markedNames(row),
    ['Groups', 'Id', 'Label', 'Name', 'Tag'],
    'the new row marks identifier, label, name, the Groups list, and the tag',
  );
});

test('a new unit row marks its label and leaves its identifier plain', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-units'));

  assert.deepEqual(markedNames(row), ['Label'], 'the identifier is required only when the row carries no range, so it is not marked');
});

// ---------------------------------------------------------------------------
// A saved row carries the same marks as a new one: the mark names the rule, not the row's state.
// ---------------------------------------------------------------------------

test('a saved row carries the same marks as the row the operator added', () => {
  seed(getAdminContext().store);

  const sites = attach('rdio-admin-sites');
  const added = addRow(sites);

  const saved = rows(sites).find((row) => row !== added);

  assert.deepEqual(markedNames(saved), markedNames(added), 'the marks answer the fields, not the values in them');

  const talkgroups = attach('rdio-admin-talkgroups');
  const addedTalkgroup = addRow(talkgroups);
  const savedTalkgroup = rows(talkgroups).find((row) => row !== addedTalkgroup);

  assert.deepEqual(markedNames(savedTalkgroup), markedNames(addedTalkgroup), 'the saved talkgroup marks the same five names');
});

// ---------------------------------------------------------------------------
// The names the row shows, so a mark missing from a field nobody reads is caught by the pair.
// ---------------------------------------------------------------------------

test('the talkgroup row shows the names the mark is read against', () => {
  seed(getAdminContext().store);

  const row = addRow(attach('rdio-admin-talkgroups'));

  assert.deepEqual(
    shownNames(row),
    ['Audio alert', 'Delay', 'Frequency', 'Groups', 'Id', 'Label', 'Led Color', 'Name', 'Tag', 'Type'],
    'the row names ten fields, five of them marked',
  );

  assert.ok(fieldOf(row, 'Id'), 'the marked name is the field the operator types the identifier into');
});
