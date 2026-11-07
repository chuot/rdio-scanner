// Blacklist in a section that cannot be saved: one incomplete row disables Save
// so a merely queued entry could never be stored; the click must refuse
// cleanly.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-talkgroups.js';

import { findAll } from './dom-shim.js';

const requests = [];

globalThis.fetch = async (url, options = {}) => {
  requests.push({ url, options });

  return { ok: true, status: 200, json: async () => [] };
};

function systems() {
  return [
    {
      id: 1,
      label: 'Grand Metro',
      order: 1,
      systemRef: 10001,
      autoPopulate: true,
      blacklists: '1514200',
      sites: [{ id: 4, label: 'Metro East', order: 1, siteRef: 101 }],
      talkgroups: [
        { id: 1, label: 'Ground', name: 'Dispatch 1', talkgroupRef: 1514000, order: 1, groupIds: [1], tagId: 2 },
        // the unfinished row (no tag) makes the section refuse to save and the bar disable
        { id: 2, label: 'Ground', name: 'Dispatch 2', talkgroupRef: 1514100, order: 2, groupIds: [1] },
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

test('blacklisting a talkgroup in a section the page will not save stores nothing and says which fields must be fixed', async () => {
  requests.length = 0;

  const store = seed();
  const el = talkgroupsSection();

  // The section's own Save answers disabled: the incomplete row is the section's state, not the
  // operator's reading of it.
  assert.equal(saveControl(el).disabled, true, 'the section saved its incomplete talkgroup row');

  blacklistButton(el).click();

  await settled();

  assert.equal(requests.filter((entry) => entry.url === '/api/admin/config/systems/1/talkgroups').length, 0,
    'the Blacklist action sent a request for a section the page refuses to save');

  const slice = store.getState()['subsection:talkgroups:1'];

  assert.deepEqual(slice.working.map((row) => row.talkgroupRef), [1514000, 1514100],
    'the Blacklist action removed the talkgroup row from a section it did not store');
  assert.equal(slice.dirty, false, 'the Blacklist action left an unsaved change in a section it refused to store');
  assert.equal(store.getState()['blacklists:1'].working, '1514200',
    'the Blacklist action added a blacklist entry to a section it refused to store');
  assert.equal(store.getState()['blacklists:1'].dirty, false,
    'the Blacklist action left a blacklist entry pending in a section it refused to store');

  const message = snackbarText();

  assert.ok(message, 'the Blacklist action refused without telling the operator');
  assert.ok(message.includes('Tag is required'),
    `the Blacklist action did not name the fields that must be fixed, it said "${message}"`);
});
