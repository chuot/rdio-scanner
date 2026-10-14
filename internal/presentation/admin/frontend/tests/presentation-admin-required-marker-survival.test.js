// The marks survive the page's own work. A section rebuilds its rows for a great many reasons: the
// operator adds a row, deletes one, drags one above another, opens a row and closes it, picks another
// system above a per-level section, discards the section, and the server's answer replaces the working
// copy under the section. Each of those rebuilds hands the fields back to the page, and a mark that is
// a declaration survives all of them: it is on the name of the field, and the name is the same name the
// section built before the rebuild.
//
// Each reading takes the marked set of a section, does one of those things to the page, and reads the
// marked set again.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-systems.js';
import '../presentation/admin/rdio-admin-sites.js';
import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-units.js';
import '../presentation/admin/rdio-admin-groups.js';
import '../presentation/admin/rdio-admin-access.js';
import '../presentation/admin/rdio-admin-options.js';

import { connectFields, findAll, requiredMark } from './dom-shim.js';

const METRO = { id: 1, label: 'Metro', order: 2, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };
const COUNTY = { id: 2, label: 'County', order: 1, systemRef: 2, blacklists: '', sites: [], talkgroups: [], units: [] };

const SITE_ROW = (id) => ({ id, label: `Site ${id}`, siteRef: id, order: 1 });
const TALKGROUP_ROW = (id) => ({ id, label: `Fire ${id}`, name: `Fire dispatch ${id}`, talkgroupRef: id, order: 1, groupIds: [1], tagId: 3, alert: '', delay: null, frequency: null, led: '', type: '' });
const UNIT_ROW = (id) => ({ id, label: `Car ${id}`, unitRef: 100 + id, unitFrom: 100 + id, unitTo: 199 + id, order: 1 });

const SITES = { 1: [SITE_ROW(4)], 2: [SITE_ROW(5)] };
const TALKGROUPS = { 1: [TALKGROUP_ROW(7)], 2: [TALKGROUP_ROW(8)] };
const UNITS = { 1: [UNIT_ROW(9)], 2: [UNIT_ROW(10)] };

function slice(working) {
  return { baseline: structuredClone(working), working: structuredClone(working), dirty: false, invalid: false, conflict: false };
}

function seed(store) {
  const state = { auth: { authenticated: true, docker: false, passwordNeedChange: false }, alerts: { fire: {} } };

  for (const [level, bySystem] of [['sites', SITES], ['talkgroups', TALKGROUPS], ['units', UNITS]]) {
    for (const system of [COUNTY, METRO]) {
      state[`subsection:${level}:${system.id}`] = slice(bySystem[system.id]);
    }
  }

  store.setState({
    ...state,
    'section:systems': slice([METRO, COUNTY]),
    'section:groups': slice([{ id: 1, label: 'Dispatch', alert: '', led: '' }]),
    'section:access': slice([{ id: 1, order: 1, code: 'alpha', ident: 'AAA', systems: '*' }]),
    'section:options': slice({ audioConversion: 'opus', autoPopulate: false, branding: 'Ridio', disableDuplicateDetection: 0, email: 'ops@example.test', keypadBeeps: 'standard', maxClients: 5, pruneDays: 7, showListenersCount: 0, sortTalkgroups: false }),
  });

  return store;
}

function attach(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

function rows(el) {
  return findAll(el, (node) => node.tag === 'details'
    || (node.tag === 'div' && node.className?.split(/\s+/).includes('rdio-admin-row')));
}

function addRow(el) {
  findAll(el, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes('add'))[0].click();
  connectFields(el);

  return rows(el)[0];
}

function rowButton(row, label) {
  return findAll(row, (node) => node.tag === 'button' && node.textContent === label)[0];
}

function barButton(el, className) {
  const bar = findAll(el, (node) => node.tag === 'rdio-admin-section-bar')[0];

  bar.connectedCallback();

  return findAll(bar, (node) => node.tag === 'button' && node.className.split(/\s+/).includes(className))[0];
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

// The system above a per-level section: the operator picks another system and the section renders the
// rows of the one they picked.
function switchSystem(el) {
  const select = findAll(el, (node) => node.tag === 'rdio-select')[0];

  select.dispatchEvent({ type: 'change', detail: String(el.systemId === 1 ? 2 : 1) });
  connectFields(el);
}

// The working copy replaced from outside the section: the server's answer, the reload of a level, the
// save that hands the section a copy of its own rows.
function replaceWorking(el, key, rows) {
  getAdminContext().store.setState({ [key]: slice(rows) });
  connectFields(el);
}

function spanClass(node) {
  return node.className?.split(/\s+/).find((name) => name === 'field-title' || name === 'body');
}

function markedNames(root) {
  const names = new Set();

  for (const node of findAll(root, (child) => child.tag === 'rdio-form-field')) {
    const label = findAll(node, (child) => child.tag === 'label')[0];

    if (label && requiredMark(label)) names.add(label.textContent);
  }

  for (const span of findAll(root, (child) => spanClass(child))) {
    if (requiredMark(span)) names.add(span.textContent);
  }

  return [...names].sort();
}

const OPERATIONS = {
  'adding a row': (el) => { addRow(el); },
  'deleting a row': (el, survivor) => { rowButton(rows(el)[0], survivor.deleteLabel).click(); connectFields(el); },
  'dragging a row above another': (el) => { dragRow(el, 0, 1); connectFields(el); },
  'opening and closing a row': (el) => { const row = rows(el)[0]; row.open = true; row.open = false; connectFields(el); },
  'picking another system': (el) => { switchSystem(el); },
  'discarding the section': (el) => { barButton(el, 'discard').click(); connectFields(el); },
};

const SURVIVORS = [
  {
    section: 'systems',
    tag: 'rdio-admin-systems',
    storeKey: () => 'section:systems',
    deleteLabel: 'Delete system',
    expected: ['Id', 'Label'],
    operations: ['adding a row', 'deleting a row', 'dragging a row above another', 'opening and closing a row', 'discarding the section'],
    replaced: () => [METRO, COUNTY, { id: 3, label: 'Valley', order: 3, systemRef: 3, blacklists: '', sites: [], talkgroups: [], units: [] }],
  },
  {
    section: 'talkgroups',
    tag: 'rdio-admin-talkgroups',
    storeKey: (el) => `subsection:talkgroups:${el.systemId}`,
    deleteLabel: 'Delete talkgroup',
    expected: ['Groups', 'Id', 'Label', 'Name', 'Tag'],
    operations: ['adding a row', 'deleting a row', 'dragging a row above another', 'opening and closing a row', 'picking another system', 'discarding the section'],
    replaced: () => [TALKGROUP_ROW(11), TALKGROUP_ROW(12)],
  },
  {
    section: 'units',
    tag: 'rdio-admin-units',
    storeKey: (el) => `subsection:units:${el.systemId}`,
    deleteLabel: 'Delete unit',
    expected: ['Label'],
    operations: ['adding a row', 'deleting a row', 'dragging a row above another', 'opening and closing a row', 'picking another system', 'discarding the section'],
    replaced: () => [UNIT_ROW(11), UNIT_ROW(12)],
  },
  {
    section: 'access',
    tag: 'rdio-admin-access',
    storeKey: () => 'section:access',
    deleteLabel: 'Delete access',
    expected: ['Code', 'Ident'],
    operations: ['adding a row', 'deleting a row', 'dragging a row above another', 'discarding the section'],
    replaced: () => [{ id: 1, order: 1, code: 'alpha', ident: 'AAA', systems: '*' }, { id: 2, order: 2, code: 'bravo', ident: 'BBB', systems: '1' }],
  },
  {
    section: 'groups',
    tag: 'rdio-admin-groups',
    storeKey: () => 'section:groups',
    deleteLabel: 'Delete Group',
    expected: ['Label'],
    operations: ['adding a row', 'deleting a row', 'dragging a row above another', 'discarding the section'],
    replaced: () => [{ id: 1, label: 'Dispatch', alert: '', led: '' }, { id: 2, label: 'Tac', alert: '', led: '' }],
  },
  {
    section: 'options',
    tag: 'rdio-admin-options',
    storeKey: () => 'section:options',
    hasRows: false,
    expected: ['Keypad Beep Style', 'Max Clients', 'Prune Days'],
    operations: ['discarding the section'],
    replaced: () => ({ audioConversion: 'opus', autoPopulate: true, branding: 'Scanner', disableDuplicateDetection: 1, email: 'dispatch@example.test', keypadBeeps: 'whistler', maxClients: 9, pruneDays: 14, showListenersCount: 1, sortTalkgroups: true }),
  },
];

for (const survivor of SURVIVORS) {
  const { section, tag, expected, operations, replaced, storeKey, hasRows = true } = survivor;

  for (const operation of operations) {
    test(`the marks on the ${section} section survive ${operation.toLowerCase()}`, () => {
      seed(getAdminContext().store);

      const el = attach(tag);

      if (hasRows) addRow(el);

      assert.deepEqual(markedNames(el), expected, `the ${section} section marks its fields before the operator ${operation.toLowerCase()}`);

      OPERATIONS[operation](el, survivor);

      assert.deepEqual(markedNames(el), expected, `the marks are the same names after the operator ${operation.toLowerCase()}`);
    });
  }

  test(`the marks on the ${section} section survive the working copy being replaced from outside`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);

    if (hasRows) addRow(el);

    assert.deepEqual(markedNames(el), expected, `the ${section} section marks its fields before the copy is replaced`);

    replaceWorking(el, storeKey(el), replaced());

    assert.deepEqual(markedNames(el), expected, 'the marks are the same names on the rows the section was handed');
  });
}
