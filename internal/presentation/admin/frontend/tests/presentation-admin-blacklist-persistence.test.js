// blacklisting is two stored halves meant as one: row removed, ref refused the
// entry names the talkgroup reference the server compares, not the store's id.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-talkgroups.js';

import { sectionWorking } from '../presentation/admin/section-shell.js';

import { findAll, keystroke, reachField } from './dom-shim.js';

// the test records the request the page's own api adapter sends; no
// server.
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

  // a loaded configuration holds every level of every system,
  // plus the blacklist the talkgroups section edits beside its rows
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
    // valid rows leave the save bar in its settled answer
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

// reads the blacklist the page holds for this system, not where it is kept
function blacklistText(store, systemId) {
  const slice = store.getState()[`blacklists:${systemId}`];

  if (slice) return slice.working ?? '';

  return sectionWorking('systems')?.find((system) => system.id === systemId)?.blacklists ?? '';
}

// a level save payload is the rows plus the blacklist text;
// a bare array is rows alone
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

test('blacklisting a talkgroup writes the reference the server compares, not the identifier the store gave the row', () => {
  const store = seed();

  const el = talkgroupsSection();

  const button = blacklistButton(el);

  assert.ok(button, 'the talkgroups section has no Blacklist action on a saved talkgroup row');

  button.click();

  assert.equal(blacklistText(store, 1), '1514000', 'the blacklist entry the action wrote names the row identifier, not the talkgroup reference');
});

// one Blacklist click on a saved talkgroup stores both halves with Save untouched
// so the reading is what the click alone leaves stored
async function blacklistOneTalkgroup({ edit } = {}) {
  requests.length = 0;

  const store = seed();
  const el = talkgroupsSection();

  if (edit) edit(el);

  blacklistButton(el).click();

  await settled();

  const level = requests.filter((entry) => entry.url === '/api/admin/config/systems/1/talkgroups');
  const body = level.length === 1 ? JSON.parse(level[0].options.body) : null;

  return { store, level, body };
}

// a Label edit in a row that is not blacklisted is what the click must store too
function labelControl(el, index) {
  const row = el.querySelectorAll('.rdio-admin-row')[index];
  const field = row.querySelectorAll('rdio-form-field').find((f) => f.querySelector('label')?.textContent === 'Label');

  return field?.querySelector('input');
}

test('blacklisting a talkgroup stores the row removal and the blacklist entry in the click, with no save of the section', async () => {
  const { store, level, body } = await blacklistOneTalkgroup();

  assert.equal(level.length, 1, 'the Blacklist action stored nothing: it sent no request to the talkgroups level');

  assert.ok(body, 'the Blacklist action sent a body the test cannot read');
  assert.deepEqual(rowsOf(body).map((row) => row.talkgroupRef), [1514100], 'the rows the Blacklist action stored are not the rows left after the talkgroup was blacklisted');
  assert.equal(blacklistsOf(body), '1514000', 'the Blacklist action did not store the blacklist entry naming the talkgroup reference');

  const slice = store.getState()['subsection:talkgroups:1'];

  assert.equal(slice.dirty, false, 'the Blacklist action left the talkgroups marked as unsaved');
  assert.equal(store.getState()['blacklists:1'].working, '1514000', 'after the Blacklist action the page does not hold the blacklist entry it stored');
  assert.equal(store.getState()['blacklists:1'].dirty, false, 'the Blacklist action left the blacklist entry marked as unsaved');
});

test('blacklisting a talkgroup stores the talkgroup edits the operator already made in the same section', async () => {
  const { body } = await blacklistOneTalkgroup({
    edit: (el) => {
      const label = labelControl(el, 1);

      keystroke(label, 'Dispatch 2 renamed');
      reachField(label);
    },
  });

  assert.ok(body, 'the Blacklist action stored nothing: it sent no request to the talkgroups level');
  assert.deepEqual(rowsOf(body).map((row) => row.label), ['Dispatch 2 renamed'], 'the Blacklist action did not store the talkgroup label the operator had already edited');
});
