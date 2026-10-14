// One row open at a time in the systems tree.
//
// The operator edits one talkgroup at a time. Opening a row shrinks the rows that were open, so the
// list stays short and the save bar under it is reached without scrolling past the rows opened on the
// way. These readings are taken from the rendered rows: the panel that is open, the message under the
// control, the warning icon on the row's summary line, and the store's working copy under them.
//
// The rows are driven through the event a summary activation produces — the toggle event a click and
// the keyboard's Enter and Space both leave on the row. The real Enter and Space are walked in the
// browser harness; the shim has no keyboard.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-systems.js';
import '../presentation/admin/rdio-admin-sites.js';
import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-units.js';
import '../presentation/admin/rdio-admin-tools.js';
import '../presentation/admin/openapi-box.js';

import { connectFields, fieldMessage, findAll, reachField, texts } from './dom-shim.js';

const METRO = { id: 1, label: 'Metro', order: 2, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };
const COUNTY = { id: 2, label: 'County', order: 1, systemRef: 2, blacklists: '', sites: [], talkgroups: [], units: [] };
const DEPOT = { id: 3, label: 'Depot', order: 3, systemRef: 3, blacklists: '', sites: [], talkgroups: [], units: [] };

const SITES = {
  1: [],
  2: [
    { id: 4, label: 'East', siteRef: 40, order: 1 },
    { id: 5, label: 'West', siteRef: 50, order: 2 },
    { id: 6, label: 'North', siteRef: 60, order: 3 },
  ],
};

const TALKGROUPS = {
  1: [],
  2: [
    { id: 7, label: 'Fire', name: 'Fire dispatch', talkgroupRef: 70, order: 1, groupIds: [1], tagId: 3, alert: '', delay: null, frequency: null, led: '', type: '' },
    { id: 8, label: 'Tac', name: 'Tac dispatch', talkgroupRef: 80, order: 2, groupIds: [1], tagId: 3, alert: '', delay: null, frequency: null, led: '', type: '' },
    { id: 10, label: 'Car', name: 'Car dispatch', talkgroupRef: 100, order: 3, groupIds: [1], tagId: 3, alert: '', delay: null, frequency: null, led: '', type: '' },
  ],
};

const UNITS = {
  1: [],
  2: [
    { id: 9, label: 'Car', unitRef: 100, unitFrom: 100, unitTo: 199, order: 1 },
    { id: 11, label: 'Truck', unitRef: 110, unitFrom: 110, unitTo: 119, order: 2 },
    { id: 12, label: 'Boat', unitRef: 120, unitFrom: 120, unitTo: 129, order: 3 },
  ],
};

const GROUPS = [{ id: 1, label: 'Dispatch' }];
const TAGS = [{ id: 3, label: 'Fire' }];

function slice(baseline) {
  return { baseline, working: structuredClone(baseline), dirty: false, invalid: false, conflict: false };
}

function seed(store = getAdminContext().store) {
  const state = { auth: { authenticated: true, docker: false, passwordNeedChange: false }, alerts: {} };

  for (const [level, bySystem] of [['sites', SITES], ['talkgroups', TALKGROUPS], ['units', UNITS]]) {
    for (const system of [COUNTY, METRO, DEPOT]) {
      state[`subsection:${level}:${system.id}`] = slice(bySystem[system.id] ?? []);
    }
  }

  store.setState({
    ...state,
    'section:systems': slice([METRO, COUNTY, DEPOT]),
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

// The rows on the page, in the order the section presents them.
function rows(el) {
  return findAll(el, (node) => node.tag === 'details' && node.dataset.key !== undefined);
}

// The rows the operator sees open. The reading is the page, not the section's map: the map is what the
// next render reproduces the page from.
function openRows(el) {
  return rows(el).filter((row) => row.open);
}

function openTitles(el) {
  return openRows(el).map(rowTitle);
}

function rowTitle(row) {
  return texts(findAll(row, (node) => node.className?.split(/\s+/).includes('row-title')))[0] ?? '';
}

// The summary activation: the row's open state flips, and the row answers with the toggle event a
// click and the keyboard's Enter and Space both produce. The browser leaves that event on the row the
// operator activated — it does not bubble it up to the list the rows sit on — so whatever answers it
// has to answer it on the row.
function activate(row) {
  row.open = !row.open;
  row.dispatchEvent({ type: 'toggle', target: row, bubbles: false });
}

function control(el, className) {
  return findAll(el, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes(className))[0];
}

function addRow(el) {
  control(el, 'add').click();
  connectFields(el);

  return rows(el)[0];
}

// The name a field answers to, and the control inside it.
function field(row, title) {
  return findAll(row, (node) => node.tag === 'rdio-form-field').find(
    (node) => texts(findAll(node, (child) => child.tag === 'label'))[0] === title,
  );
}

function controlOf(row, title) {
  return findAll(field(row, title), (node) => ['input', 'textarea', 'rdio-select'].includes(node.tag))[0];
}

// The operator clears the field and walks away from it: the field reveals its message, and the row
// gains its warning icon.
function revealEmptyField(row, title) {
  const input = controlOf(row, title);

  input.value = '';
  input.dispatchEvent({ type: 'change' });
  connectFields(row);

  return input;
}

function markerOn(row) {
  return findAll(row, (node) => node.className?.split(/\s+/).includes('row-invalid')).length;
}

function summaries(row) {
  return findAll(row, (node) => node.tag === 'summary');
}

// The server replaces the rows under the operator's feet: the section rebuilds them from the state it
// keeps, which is the state the operator's clicks left.
function pushRows(store, key, rows) {
  const current = store.getState()[key];

  store.setState({
    [key]: {
      ...current,
      baseline: rows.map((row) => ({ ...row })),
      working: rows.map((row) => ({ ...row })),
      dirty: false,
      invalid: false,
      conflict: false,
    },
  });
}

// The four systems-tree sections: the three per-level sections and the systems section. Each shows
// three rows of the system it opens on, and each saves on its own slice.
const SECTIONS = [
  ['talkgroups', 'rdio-admin-talkgroups', 'subsection:talkgroups:2'],
  ['sites', 'rdio-admin-sites', 'subsection:sites:2'],
  ['units', 'rdio-admin-units', 'subsection:units:2'],
  ['systems', 'rdio-admin-systems', 'section:systems'],
];

// The list a browser hands the section is a NodeList: it carries its rows by index, `forEach`, and
// iteration, and it carries no `filter`, `map`, or `find`. The shim answers with an array, so the
// shape the operator's browser has is stated here for the reading to reach the rule the way it
// reaches it on the page.
function asBrowserRowList(found) {
  return {
    length: found.length,
    item: (i) => found[i] ?? null,
    forEach: (fn, thisArg) => found.forEach(fn, thisArg),
    [Symbol.iterator]: () => found[Symbol.iterator](),
  };
}

function listTheBrowserHands(el) {
  const list = findAll(el, (node) => node.className?.split(/\s+/).includes('rdio-admin-rows'))[0];

  assert.ok(list, 'the section carries the list its rows sit on');

  const read = list.querySelectorAll.bind(list);

  list.querySelectorAll = (selector) => {
    const found = read(selector);

    return selector.includes('details[data-key]') ? asBrowserRowList(found) : found;
  };

  return list;
}

// CA-001, CA-008: opening a row closes the rows that were open, and the row the operator opened is
// the one row the section shows open.
for (const [name, tag, key] of SECTIONS) {
  test(`opening a second ${name} row closes the first and leaves one row open`, () => {
    seed();

    const el = attach(tag);

    assert.equal(rows(el).length, 3, `the ${name} section shows three rows`);
    assert.equal(openRows(el).length, 0, 'the section opens with every row collapsed');

    const [first, second] = rows(el);

    activate(first);

    assert.deepEqual(openTitles(el), [rowTitle(first)], 'the row the operator opened is open');

    activate(second);

    assert.equal(first.open, false, 'opening a row closes the row the operator left open');
    assert.equal(second.open, true, 'the row the operator opened is open');
    assert.equal(openRows(el).length, 1, `the ${name} list holds exactly one open row`);
    assert.equal(openRows(el)[0], second);
  });

  test(`opening a ${name} row from the keyboard closes the rest and states the row is expanded`, () => {
    seed();

    const el = attach(tag);

    const [first, second, third] = rows(el);

    activate(first);
    // The keyboard's Enter and Space produce the same activation event a click produces: the reading
    // drives that event, and the harness walks the real keys.
    activate(second);
    activate(third);

    for (const row of [first, second]) {
      assert.equal(row.open, false, 'the rows the operator opened earlier are collapsed');
    }

    assert.equal(third.open, true, 'the row the operator last opened is open');

    for (const row of rows(el)) {
      assert.equal(summaries(row).length, 1, 'every row presents the summary the operator activates');
      assert.equal(row.open, row === third, `the ${rowTitle(row)} row states whether it is expanded`);
    }

    assert.equal(openRows(el).length, 1);
  });

  test(`a ${name} rebuild with one row open shows exactly one open row`, () => {
    const store = seed();

    const el = attach(tag);

    const [first, second] = rows(el);

    activate(first);
    activate(second);

    // The rows are rebuilt from the state the operator's clicks left: the render must not leave two
    // rows open, and must not open a row the operator never opened.
    pushRows(store, key, store.getState()[key].working.map((row) => ({ ...row, order: row.order })));

    assert.equal(openRows(el).length, 1, 'the rebuild reproduces exactly one open row');
    assert.equal(openRows(el)[0], rows(el)[1], 'the rebuild keeps the row the operator opened open');
    assert.equal(rowTitle(rows(el)[1]), rowTitle(second));
  });
}

// CA-003: the row the add control creates is the row the operator came to fill in, so it is the
// section's one open row and the rows the operator left open close.
for (const [name, tag] of [['talkgroup', 'rdio-admin-talkgroups'], ['site', 'rdio-admin-sites'], ['unit', 'rdio-admin-units'], ['system', 'rdio-admin-systems']]) {
  test(`pressing the new ${name} control opens the row it creates and closes the row that was open`, () => {
    seed();

    const el = attach(tag);

    const [first] = rows(el);

    activate(first);

    assert.equal(openRows(el).length, 1, 'the operator has one row open before they press the add control');

    const fresh = addRow(el);

    assert.equal(fresh.open, true, 'the row the add control creates is open on its fields');
    assert.equal(openRows(el).length, 1, `the ${name} list holds exactly one open row after the add`);
    assert.equal(openRows(el)[0], fresh, 'the open row is the row the add control created');
    assert.equal(first.open, false, 'the row the operator left open closes');
    assert.equal(rowTitle(first), rowTitle(rows(el)[1]), 'the row the operator left open keeps its place in the list');
  });
}

// CA-004: shrinking is presentation only. Reading a row is not a change to the section's working copy,
// and the save bar answers the same as it did before the clicks.
for (const [name, tag, key] of [['talkgroups', 'rdio-admin-talkgroups', 'subsection:talkgroups:2'], ['sites', 'rdio-admin-sites', 'subsection:sites:2'], ['units', 'rdio-admin-units', 'subsection:units:2']]) {
  test(`opening and closing ${name} rows writes nothing to the section's working copy`, () => {
    const store = seed();

    const el = attach(tag);

    const before = store.getState()[key];
    const working = before.working;
    const snapshot = JSON.stringify(working);

    for (const row of rows(el)) activate(row);
    for (const row of rows(el)) activate(row);

    const after = store.getState()[key];

    assert.equal(after.working, working, 'the working copy is the same object: opening a row commits nothing');
    assert.equal(JSON.stringify(after.working), snapshot, 'no row field changes when rows open and close');
    assert.equal(after.dirty, before.dirty, 'the dirty flag answers the same as it did before the clicks');
    assert.equal(after.dirty, false, 'reading rows does not enable the save bar');

    assert.equal(rows(el).length, 3, 'the rows the operator is working on stay on the page');
  });
}

// CA-004, the other half: the message the operator revealed in a row, and the row's invalid marker,
// survive being collapsed by opening another row, and are there when the row is opened again.
for (const [name, tag] of [['talkgroup', 'rdio-admin-talkgroups'], ['site', 'rdio-admin-sites'], ['unit', 'rdio-admin-units'], ['system', 'rdio-admin-systems']]) {
  test(`a collapsed ${name} row keeps the message the operator revealed and its invalid marker`, () => {
    seed();

    const el = attach(tag);

    const [first, second] = rows(el);

    activate(first);
    revealEmptyField(first, 'Label');

    assert.equal(fieldMessage(field(first, 'Label')), 'Label is required', 'the row the operator reached carries its message');
    assert.equal(markerOn(first), 1, 'the row the operator reached shows it is invalid');

    activate(second);

    assert.equal(first.open, false, 'opening another row collapses the row carrying the message');
    assert.equal(markerOn(first), 1, 'the warning icon rides the summary line of the collapsed row');
    assert.equal(openRows(el).length, 1, 'the collapsed row does not stay open to keep its message');

    activate(first);

    assert.equal(first.open, true, 'the operator opens the row again');
    assert.equal(fieldMessage(field(first, 'Label')), 'Label is required', 'the message is on the row when it is opened again');
    assert.equal(markerOn(first), 1, 'the invalid marker is on the row when it is opened again');
  });
}

// CA-009: the rule mounts on the systems-tree lists, and reaches nothing else. The Tools panels are
// five actions the operator reads together — they open *Import talkgroups CSV* while *Export config*
// stays open. The OpenAPI endpoint rows are a table of contents the operator reads down: collapsing
// every endpoint when one opens turns a reference into a single-item lookup.
const TOOLS_SEED = {
  auth: { authenticated: true, docker: false, passwordNeedChange: false },
  alerts: {},
};

function seedTools() {
  const { store } = getAdminContext();

  const state = { ...TOOLS_SEED };

  for (const name of ['systems', 'groups', 'tags', 'access', 'apikeys', 'dirwatch', 'downstreams']) {
    state[`section:${name}`] = slice([]);
  }

  state['section:options'] = { baseline: null, working: null, dirty: false, invalid: false, conflict: false };

  store.setState(state);

  return attach('rdio-admin-tools');
}

function panelByTitle(el, title) {
  return findAll(el, (node) => node.tag === 'details').find((panel) => rowTitle(panel) === title);
}

const ENDPOINTS = {
  title: 'Systems',
  icon: 'settings',
  endpoints: [
    { method: 'GET', path: '/api/systems', summary: 'List the systems' },
    { method: 'GET', path: '/api/config', summary: 'Read the configuration' },
    { method: 'GET', path: '/api/systems/{id}', summary: 'Read one system' },
  ],
};

function endpointRow(box, path) {
  return findAll(box, (node) => node.tag === 'details').find(
    (row) => texts(findAll(row, (node) => node.className === 'endpoint-path')).map((text) => text.replace(/\u200B/g, '')).includes(path),
  );
}

test('the Tools panels keep opening side by side', () => {
  const el = seedTools();

  const exportPanel = panelByTitle(el, 'Export config');
  const importPanel = panelByTitle(el, 'Import config');

  activate(exportPanel);
  activate(importPanel);

  assert.equal(exportPanel.open, true, 'Export config stays open while the operator opens the panel beside it');
  assert.equal(importPanel.open, true, 'Import config opens beside it, not in place of it');

  const panels = findAll(el, (node) => node.tag === 'details');

  assert.equal(panels.filter((panel) => panel.open).length, 2, 'the Tools card holds every panel the operator opened');

  for (const panel of panels) {
    assert.equal(panel.dataset.key, undefined, 'a Tools panel is not a systems-tree row: the rule has no key to read');
  }
});

test('the OpenAPI endpoint rows keep opening side by side', () => {
  const box = new (customElements.get('rdio-admin-openapi-box'))();

  box.group = ENDPOINTS;
  box.connectedCallback();

  const [first, second, third] = ['/api/systems', '/api/config', '/api/systems/{id}'].map((path) => endpointRow(box, path));

  activate(first);
  activate(third);

  assert.equal(first.open, true, 'the endpoint the operator opened first stays open');
  assert.equal(third.open, true, 'the endpoint the operator opened last is open beside it');
  assert.equal(findAll(box, (node) => node.tag === 'details').filter((row) => row.open).length, 2, 'the endpoint list keeps every row the operator opened');

  for (const row of [first, second, third]) {
    assert.equal(row.dataset.key, undefined, 'an endpoint row is not a systems-tree row: the rule has no key to read');
  }
});

// CA-001, CA-008, on the list the operator's browser hands the section. The rule reads the rows the
// operator left open off the page; the page answers with a NodeList, and a NodeList that is asked for
// `filter` answers nothing, which leaves every row the operator opened sitting open on the screen.
for (const [name, tag] of SECTIONS) {
  test(`a ${name} row opened on the list a browser hands the section closes the rows that were open`, () => {
    seed();

    const el = attach(tag);

    listTheBrowserHands(el);

    const [first, second] = rows(el);

    activate(first);
    activate(second);

    assert.equal(first.open, false, 'the row the operator left open closes: the rule read the open rows off a browser list');
    assert.equal(second.open, true, 'the row the operator opened is open');
    assert.equal(openRows(el).length, 1, `the ${name} list holds exactly one open row`);
    assert.equal(openRows(el)[0], second);
  });
}
