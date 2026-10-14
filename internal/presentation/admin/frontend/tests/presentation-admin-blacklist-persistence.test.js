// Blacklisting a talkgroup. The action writes two things the operator means as one: the talkgroup row
// goes away, and the system refuses that talkgroup. Both have to be stored by the save of the section
// the operator is standing in, and the entry has to name the number the server compares a recording
// against — the talkgroup's reference, not the identifier the store gave the row.
//
// The rows here are laid out the way the conformance fixture lays them out: the first talkgroup of the
// first system is row 1 and reference 1514000. The two numbers differ, which is what makes a reading
// that names one of them say which one the action wrote.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-talkgroups.js';

import { sectionWorking } from '../presentation/admin/section-shell.js';

import { findAll } from './dom-shim.js';

// The page reaches the server through fetch. Recording what fetch is asked to send shows the request
// a save of the section makes — the request the browser sends, through the page's own api adapter —
// with no server in the test. The answer is the answer the server gives the same request: the level
// endpoint answers the rows it stored, whether they arrived as the list of the level's rows or as the
// object carrying them beside the system's blacklist.
const requests = [];

globalThis.fetch = async (url, options = {}) => {
  requests.push({ url, options });

  const submitted = options.body ? JSON.parse(options.body) : {};

  const stored = Array.isArray(submitted) || !url.endsWith('/talkgroups') ? submitted : submitted.talkgroups;

  return {
    ok: true,
    status: 200,
    json: async () => stored,
  };
};

function systems() {
  return [
    {
      id: 1,
      label: 'Grand Metro',
      order: 1,
      systemRef: 10001,
      autoPopulate: true,
      blacklists: '',
      sites: [{ id: 4, label: 'Metro East', order: 1, siteRef: 101 }],
      talkgroups: [
        { id: 1, label: 'Ground', name: 'Dispatch 1', talkgroupRef: 1514000, order: 1, groupIds: [1], tagId: 2 },
        { id: 2, label: 'Ground', name: 'Dispatch 2', talkgroupRef: 1514100, order: 2, groupIds: [1], tagId: 2 },
      ],
      units: [{ id: 7, label: 'Car 1', order: 1, unitRef: 1001 }],
    },
  ];
}

function seed() {
  const { store } = getAdminContext();

  const state = { auth: { authenticated: true, docker: false, passwordNeedChange: false } };

  for (const level of ['sites', 'talkgroups', 'units']) {
    for (const system of systems()) {
      state[`subsection:${level}:${system.id}`] = {
        baseline: structuredClone(system[level]),
        working: structuredClone(system[level]),
        dirty: false,
        invalid: false,
        conflict: false,
      };
    }
  }

  // The state a loaded configuration leaves the page holding: every level of every system, and the
  // system's blacklist the talkgroups section edits beside its rows.
  for (const system of systems()) {
    state[`blacklists:${system.id}`] = {
      systemId: system.id,
      baseline: system.blacklists ?? '',
      working: system.blacklists ?? '',
      dirty: false,
      invalid: false,
      conflict: false,
    };
  }

  store.setState({
    ...state,
    'section:systems': { baseline: structuredClone(systems()), working: systems(), dirty: false, invalid: false, conflict: false },
    // The talkgroup rows point at a group and a tag: the rows are valid, so the level's save bar
    // answers the way it answers for a system the operator has filled in.
    'section:groups': { baseline: [{ id: 1, label: 'Dispatch' }], working: [{ id: 1, label: 'Dispatch' }], dirty: false, invalid: false, conflict: false },
    'section:tags': { baseline: [{ id: 2, label: 'Law' }], working: [{ id: 2, label: 'Law' }], dirty: false, invalid: false, conflict: false },
    alerts: {},
  });

  return store;
}

function talkgroupsSection() {
  const el = new (customElements.get('rdio-admin-talkgroups'))();

  el.connectedCallback();

  return el;
}

function blacklistButton(el) {
  return findAll(el, (node) => node.tag === 'button' && node.textContent === 'Blacklist talkgroup')[0];
}

// The save bar is its own element that renders when it is attached to the page; the shim attaches
// it here so the test can press the Save control the operator sees.
function saveButton(el) {
  const bar = findAll(el, (node) => node.tag === 'rdio-admin-section-bar')[0];

  bar.connectedCallback();

  return findAll(bar, (node) => node.tag === 'button' && node.className.split(/\s+/).includes('save'))[0];
}

// The text the page is holding as this system's blacklist: the systems section's own field today,
// the per-system slice the talkgroups level saves with from this feature on. The reading asks what
// the page is holding for this system, not where it keeps it.
function blacklistText(store, systemId) {
  const slice = store.getState()[`blacklists:${systemId}`];

  if (slice) return slice.working ?? '';

  return sectionWorking('systems')?.find((system) => system.id === systemId)?.blacklists ?? '';
}

// The payload a level save sends: the rows, and — from this feature on — the blacklist text saved
// with them. A bare array is the shape that carries rows alone.
function rowsOf(body) {
  return (Array.isArray(body) ? body : body.talkgroups) ?? [];
}

function blacklistsOf(body) {
  return Array.isArray(body) ? '' : body.blacklists ?? '';
}

// The save control's handler is async: the request it starts finishes after the click returns.
function settled() {
  return new Promise((resolve) => setImmediate(resolve));
}

// Blacklist the first talkgroup of the first system, then press Save in that level's save bar:
// the two clicks the operator makes, in the order they make them.
async function blacklistAndSave() {
  requests.length = 0;

  const store = seed();
  const el = talkgroupsSection();

  blacklistButton(el).click();
  saveButton(el).click();

  await settled();

  const level = requests.filter((entry) => entry.url === '/api/admin/config/systems/1/talkgroups');
  const body = level.length === 1 ? JSON.parse(level[0].options.body) : null;

  return { store, level, body };
}

test('blacklisting a talkgroup writes the reference the server compares, not the identifier the store gave the row', () => {
  const store = seed();

  const el = talkgroupsSection();

  const button = blacklistButton(el);

  assert.ok(button, 'the talkgroups section has no Blacklist action on a saved talkgroup row');

  button.click();

  assert.equal(blacklistText(store, 1), '1514000', 'the blacklist entry the action wrote names the row identifier, not the talkgroup reference');
});

test('one save of the talkgroups level stores the rows and the blacklist together', async () => {
  const { level, body } = await blacklistAndSave();

  assert.equal(level.length, 1, 'the save of the talkgroups level did not send exactly one request to the level');
  assert.ok(body, 'the save of the talkgroups level sent a body the test can read');

  assert.deepEqual(rowsOf(body).map((row) => row.talkgroupRef), [1514100], 'the rows the level save sent are not the rows left after the blacklisting');
  assert.equal(blacklistsOf(body), '1514000', 'the save of the talkgroups level did not carry the blacklist with the rows it saved');
});

test('the save of the talkgroups level leaves nothing unsaved behind', async () => {
  const { store, body } = await blacklistAndSave();

  assert.ok(body, 'the save of the talkgroups level sent a body the test can read');

  const slice = store.getState()['subsection:talkgroups:1'];

  assert.equal(slice.dirty, false, 'the level save left the talkgroups marked as unsaved');
  assert.deepEqual(slice.working.map((row) => row.talkgroupRef), [1514100], 'the stored talkgroups still name the blacklisted row');
  assert.equal(blacklistText(store, 1), '1514000', 'after the save the page does not hold the blacklist it just stored');
  assert.equal(store.getState()['section:systems'].dirty, false, 'the talkgroups level save left the Systems section marked as unsaved');
});
