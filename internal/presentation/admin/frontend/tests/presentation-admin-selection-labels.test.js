// the names the systems-selection tree gives its rows: the row's own
// label first, the reference number after

import './dom-shim.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-access.js';
import '../presentation/admin/rdio-admin-apikeys.js';
import '../presentation/admin/rdio-admin-downstreams.js';
import '../presentation/admin/rdio-systems-selection.js';

import { connectFields, findAll, texts } from './dom-shim.js';

// the config the three sections hand the dialog: systems carry systemRef,
// talkgroups talkgroupRef; groups and tags carry only their stored id
const CONFIG = {
  groups: [{ id: 1, order: 1, label: 'Dispatch' }, { id: 2, order: 2, label: 'Interop' }],
  tags: [{ id: 3, order: 1, label: 'Fire' }],
  systems: [
    {
      systemRef: 10,
      label: 'Metro',
      talkgroups: [{ talkgroupRef: 100, label: 'Fire dispatch' }, { talkgroupRef: 101, label: 'Tac dispatch' }],
    },
    { systemRef: 11, label: 'County', talkgroups: [{ talkgroupRef: 110, label: 'Sheriff' }] },
  ],
};

function tree(config = CONFIG, value = null) {
  document.body.replaceChildren();

  const el = document.createElement('rdio-systems-selection');

  el.setup(config, value);

  document.body.append(el);

  return el;
}

// every tree row in the order the page presents it: its identity
// and the words it says
function rows(el) {
  return findAll(el, (node) => node.tag === 'input' && node.type === 'checkbox').map((box) => ({
    node: box.dataset?.node,
    text: texts(findAll(box.parent ?? box, (child) => child.tag === 'span')).join(''),
  }));
}

function rowTexts(named) {
  return named.map((row) => row.text);
}

function textOf(el, name) {
  return rows(el).find((row) => row.node === name)?.text ?? null;
}

function named(el) {
  return Object.fromEntries(rows(el).map((entry) => [entry.node, entry.text]));
}

// The names a deployment gives its rows, read as the operator reads them: top of the tree to bottom.
describe('presentation/admin/rdio-systems-selection — the rows name themselves', () => {
  it('names every row of the tree by its label, with the reference number after it', () => {
    assert.deepEqual(rowTexts(rows(tree())), [
      'Everything',
      'Dispatch',
      'Interop',
      'Fire',
      'Metro (10)',
      'Fire dispatch (100)',
      'Tac dispatch (101)',
      'County (11)',
      'Sheriff (110)',
    ]);
  });

  it('pairs each name with the row it belongs to', () => {
    assert.deepEqual(named(tree()), {
      everything: 'Everything',
      'group:1': 'Dispatch',
      'group:2': 'Interop',
      'tag:3': 'Fire',
      'system:10': 'Metro (10)',
      'talkgroup:100': 'Fire dispatch (100)',
      'talkgroup:101': 'Tac dispatch (101)',
      'system:11': 'County (11)',
      'talkgroup:110': 'Sheriff (110)',
    });
  });

  it('names a row by its label before it names it by its number', () => {
    const bare = rows(tree()).filter((row) => row.text === String(row.node).split(':')[1]);

    assert.deepEqual(bare, [], `the tree names rows by number: ${bare.map((row) => `${row.node} = ${row.text}`).join(', ')}`);
  });

  // two systems sharing a label stay distinguishable by the number after it
  it('keeps two systems that share their label distinguishable by the number after it', () => {
    const el = tree({
      groups: [],
      tags: [],
      systems: [
        { systemRef: 20, label: 'Metro', talkgroups: [] },
        { systemRef: 21, label: 'Metro', talkgroups: [] },
      ],
    }, '*');

    assert.deepEqual(rowTexts(rows(el)).filter((text) => text !== 'Everything'), ['Metro (20)', 'Metro (21)']);
  });
});

// a row with nothing filled in yet still says something an operator
// can act on, never undefined, null, or empty parentheses
describe('presentation/admin/rdio-systems-selection — a row with nothing to say says something true', () => {
  it('names a system the add control created, which has no reference yet', () => {
    const el = tree({ groups: [], tags: [], systems: [{ systemRef: null, label: '', talkgroups: [] }] });

    assert.equal(textOf(el, 'system:null'), 'New system');
  });

  it('names a group and a tag the add control created, which have no identifier at all', () => {
    const el = tree({
      groups: [{ alert: '', label: '', led: '', order: 0 }],
      tags: [{ alert: '', label: '', led: '', order: 0 }],
      systems: [],
    });

    assert.equal(textOf(el, 'group:undefined'), 'New Group');
    assert.equal(textOf(el, 'tag:undefined'), 'New Tag');
  });

  it('names a talkgroup the add control created, which has no reference yet', () => {
    const el = tree({
      groups: [],
      tags: [],
      systems: [{ systemRef: null, label: '', talkgroups: [{ talkgroupRef: null, label: '', groupIds: [], tagId: 0 }] }],
    });

    assert.equal(textOf(el, 'talkgroup:null'), 'New talkgroup');
  });

  // The fallback the tree already has, kept on purpose: a number the operator can act on is better than
  // an empty row.
  it('names a row that carries a reference and no label by the number alone', () => {
    const el = tree({
      groups: [],
      tags: [],
      systems: [{ systemRef: 12, label: '', talkgroups: [{ talkgroupRef: 120, label: '' }] }],
    });

    assert.equal(textOf(el, 'system:12'), '12');
    assert.equal(textOf(el, 'talkgroup:120'), '120');
  });

  it('trims the spaces a label arrives with, and never writes empty parentheses', () => {
    const el = tree({
      groups: [{ id: 7, order: 1, label: '  Emergency  ' }],
      tags: [{ id: 8, order: 1, label: '   ' }],
      systems: [{ systemRef: 30, label: ' Metro ', talkgroups: [{ talkgroupRef: 300, label: '   ' }] }],
    });

    assert.equal(textOf(el, 'group:7'), 'Emergency');
    assert.equal(textOf(el, 'tag:8'), 'New Tag', 'a label of spaces alone is no name for a tag');
    assert.equal(textOf(el, 'system:30'), 'Metro (30)');
    assert.equal(textOf(el, 'talkgroup:300'), '300');
  });

  it('leaves no row of the tree unnamed and no row trailing an empty bracket', () => {
    const unnamed = rows(tree({
      groups: [{ alert: '', label: '', led: '', order: 0 }, { id: 1, label: 'Dispatch' }],
      tags: [{ alert: '', label: '', led: '', order: 0 }, { id: 3, label: 'Fire' }],
      systems: [
        { systemRef: null, label: '', talkgroups: [{ talkgroupRef: null, label: '' }] },
        { systemRef: 12, label: '', talkgroups: [] },
      ],
    }));

    assert.deepEqual(unnamed.filter((row) => row.text === ''), []);
    assert.deepEqual(unnamed.filter((row) => row.text.endsWith('()')), []);
    assert.deepEqual(unnamed.filter((row) => row.text === 'undefined' || row.text === 'null'), []);
  });
});

// the three sections that open the dialog: one component answers Access,
// API Keys, and Downstreams from the sections' working copies, so an unsaved
// label names the row and the handback stays stored identifiers

const SEEDED_GROUPS = [{ id: 1, order: 1, label: 'Dispatch' }, { id: 2, order: 2, label: 'Interop' }];
const SEEDED_TAGS = [{ id: 3, order: 1, label: 'Fire' }];

// the Systems section's rows as the server hands them over: systemRef and
// talkgroupRef are business references, id the stored row identity
const SEEDED_SYSTEMS = [
  {
    id: 1, order: 1, systemRef: 10, label: 'Metro', blacklists: '', sites: [], units: [],
    talkgroups: [
      { id: 11, talkgroupRef: 100, label: 'Fire dispatch', groupIds: [1], tagId: 3 },
      { id: 12, talkgroupRef: 101, label: 'Metro tac', groupIds: [2], tagId: 0 },
    ],
  },
  {
    id: 2, order: 2, systemRef: 11, label: 'County', blacklists: '', sites: [], units: [],
    talkgroups: [{ id: 13, talkgroupRef: 110, label: 'Sheriff', groupIds: [2], tagId: 0 }],
  },
];

const SEEDED_ACCESS = [{ id: 7, order: 1, code: 'front-gate', ident: 'Front desk', limit: 0, systems: [{ id: 11, talkgroups: '*' }] }];
const SEEDED_APIKEYS = [{ id: 10, order: 1, disabled: false, key: 'key-one', ident: 'Recorder', systems: [{ id: 11, talkgroups: '*' }] }];
const SEEDED_DOWNSTREAMS = [{ id: 13, order: 1, disabled: false, url: 'https://one.example/api', apikey: 'key-one', systems: [{ id: 11, talkgroups: '*' }] }];

// the label typed in the Systems working copy and not saved yet: the
// dialog names the row with it
const EDITED_SYSTEM_LABEL = 'Metro Fire Station';

function slice(baseline) {
  return { baseline, working: structuredClone(baseline), dirty: false, invalid: false, conflict: false };
}

function seedStore() {
  const systems = structuredClone(SEEDED_SYSTEMS);

  systems[0].label = EDITED_SYSTEM_LABEL;

  getAdminContext().store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    alerts: {},
    'section:access': slice(SEEDED_ACCESS),
    'section:apikeys': slice(SEEDED_APIKEYS),
    'section:dirwatch': slice([]),
    'section:downstreams': slice(SEEDED_DOWNSTREAMS),
    'section:groups': slice(SEEDED_GROUPS),
    'section:logs': slice([]),
    'section:options': slice({}),
    'section:systems': { ...slice(systems), dirty: true },
    'section:tags': slice(SEEDED_TAGS),
  });

  return getAdminContext().store;
}

function attach(tag) {
  document.body.replaceChildren();

  const el = new (customElements.get(tag))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

// The row of the list, opened the way the operator opens it. The control that opens the dialog is inside
// the row.
function openRow(el) {
  const row = findAll(el, (node) => node.tag === 'details')[0];

  assert.ok(row, 'the section renders no row to open');

  row.open = true;
  row.dispatchEvent({ type: 'toggle', target: row, bubbles: false });

  return row;
}

function chooseSystems(row) {
  const button = findAll(row, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes('choose-systems'))[0];

  assert.ok(button, 'the row carries no Choose systems control');

  return button;
}

// The dialog the section opened: the shared box, with the tree inside it and its two actions beneath.
function openedTree() {
  return findAll(document.body, (node) => node.tag === 'rdio-systems-selection')[0] ?? null;
}

function dialogAction(label) {
  return findAll(document.body, (node) => node.tag === 'button' && node.textContent === label)[0] ?? null;
}

function boxFor(el, name) {
  return findAll(el, (node) => node.tag === 'input' && node.type === 'checkbox').find((box) => box.dataset?.node === name) ?? null;
}

function activate(box) {
  box.checked = !box.checked;
  box.dispatchEvent({ type: 'change' });
}

const SECTIONS = [
  { name: 'Access', tag: 'rdio-admin-access', key: 'section:access' },
  { name: 'API Keys', tag: 'rdio-admin-apikeys', key: 'section:apikeys' },
  { name: 'Downstreams', tag: 'rdio-admin-downstreams', key: 'section:downstreams' },
];

describe('the sections that open the dialog — the names are the labels the sections are holding', () => {
  for (const section of SECTIONS) {
    it(`${section.name}: the dialog names the label the operator typed and has not saved`, async () => {
      seedStore();

      const row = openRow(attach(section.tag));

      chooseSystems(row).dispatchEvent({ type: 'click' });

      // wait for the promise the dialog resolves on an action, so the
      // reading sees the painted section
      await new Promise((resolve) => setImmediate(resolve));

      const tree = openedTree();

      assert.ok(tree, `${section.name}: Choose systems opened no dialog`);

      assert.deepEqual(named(tree), {
        everything: 'Everything',
        'group:1': 'Dispatch',
        'group:2': 'Interop',
        'tag:3': 'Fire',
        'system:10': `${EDITED_SYSTEM_LABEL} (10)`,
        'talkgroup:100': 'Fire dispatch (100)',
        'talkgroup:101': 'Metro tac (101)',
        'system:11': 'County (11)',
        'talkgroup:110': 'Sheriff (110)',
      }, `${section.name}: the dialog the section opened does not name its rows by the labels it is holding`);
    });

    it(`${section.name}: what the dialog hands back is the identifiers it stores today`, async () => {
      const store = seedStore();

      const row = openRow(attach(section.tag));

      chooseSystems(row).dispatchEvent({ type: 'click' });

      await new Promise((resolve) => setImmediate(resolve));

      // the ticked row is the one the dialog names "Fire dispatch (100)":
      // the row shows the stored reference's number
      activate(boxFor(openedTree(), 'talkgroup:100'));

      dialogAction('Select').dispatchEvent({ type: 'click' });

      await new Promise((resolve) => setImmediate(resolve));

      assert.deepEqual(store.getState()[section.key].working[0].systems, [
        { id: 10, talkgroups: [100] },
        { id: 11, talkgroups: '*' },
      ], `${section.name}: the stored scope is no longer the identifiers it was`);
    });
  }
});
