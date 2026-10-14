// The save the Blacklist action makes, refused by the server. The action stores the talkgroup row
// removal and the blacklist entry in one request; when the server will not apply it, the operator's
// work has to survive the refusal: the row stays removed, the entry stays pending, the section's Save
// stays usable to try again, and the operator is told the save failed.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-talkgroups.js';

import { findAll } from './dom-shim.js';

const requests = [];

// The answer the server gives a save it refuses to apply: the configuration the section was saved
// against moved under it.
globalThis.fetch = async (url, options = {}) => {
  requests.push({ url, options });

  return { ok: false, status: 417, json: async () => ({}) };
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

  for (const system of systems()) {
    for (const level of ['sites', 'talkgroups', 'units']) {
      state[`subsection:${level}:${system.id}`] = {
        baseline: structuredClone(system[level]),
        working: structuredClone(system[level]),
        dirty: false,
        invalid: false,
        conflict: false,
      };
    }

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
    'section:systems': { baseline: structuredClone(systems()), working: structuredClone(systems()), dirty: false, invalid: false, conflict: false },
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

function saveControl(el) {
  const bar = findAll(el, (node) => node.tag === 'rdio-admin-section-bar')[0];

  bar.connectedCallback();

  return findAll(bar, (node) => node.tag === 'button' && node.className.split(/\s+/).includes('save'))[0];
}

function snackbarText() {
  return document.querySelector('rdio-snackbar')?.textContent ?? '';
}

function settled() {
  return new Promise((resolve) => setImmediate(resolve));
}

test('a save the server refuses keeps the blacklisting the operator asked for and says the save failed', async () => {
  requests.length = 0;

  const store = seed();
  const el = talkgroupsSection();

  blacklistButton(el).click();

  await settled();

  assert.equal(requests.filter((entry) => entry.url === '/api/admin/config/systems/1/talkgroups').length, 1,
    'the Blacklist action did not send its save to the talkgroups level');

  const slice = store.getState()['subsection:talkgroups:1'];

  assert.deepEqual(slice.working.map((row) => row.talkgroupRef), [1514100],
    'the refused save put the blacklisted talkgroup row back');
  assert.equal(slice.dirty, true, 'the refused save left the talkgroups looking saved');
  assert.equal(store.getState()['blacklists:1'].working, '1514000',
    'the refused save dropped the blacklist entry the operator asked for');
  assert.equal(store.getState()['blacklists:1'].dirty, true, 'the refused save left the blacklist entry looking saved');

  assert.equal(saveControl(el).disabled, false,
    'the refused save left the section with no way to try the save again');

  const message = snackbarText();

  assert.ok(message.includes('Unable to save the talkgroups of system 10001'),
    `the refused save did not tell the operator which system it failed on, by the number the section shows them: "${message}"`);
});

// The save the section's own bar sends, refused by the server. The operator is shown the system by
// the number the section shows them and the number they type into the fields, not by the identity
// the store gave the row.
test('the save the bar sends, refused, names the system by its reference number', async () => {
  requests.length = 0;

  seed();

  const el = talkgroupsSection();

  blacklistButton(el).click();

  await settled();

  saveControl(el).click();

  await settled();

  assert.ok(snackbarText().includes('Unable to save the talkgroups of system 10001'),
    `the refused save did not name the system by its reference number: "${snackbarText()}"`);
});
