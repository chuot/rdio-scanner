// The blacklist the operator reads. The field holds a comma separated list of talkgroup references,
// and a reference on its own tells the operator little: the number a system refuses is the number a
// recording carries, and the talkgroup it belongs to is the name the operator knows. The list beside
// the field names each entry by the talkgroup it refuses, and falls back to the reference when the
// system has no talkgroup for it — the reference stays blacklisted either way.
//
// The fixture lays the system out the way the conformance fixture does: the first talkgroup of the
// first system is reference 1514000, and the blacklist names that reference beside one the system
// has no talkgroup for.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-systems.js';

import { findAll } from './dom-shim.js';

// The page reaches the server through fetch. Recording what fetch is asked to send shows the body a
// save of the section writes, and the answer echoes back what it was given.
const requests = [];

globalThis.fetch = async (url, options = {}) => {
  requests.push({ url, options });

  return {
    ok: true,
    status: 200,
    json: async () => (options.body ? JSON.parse(options.body) : {}),
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
      blacklists: '1514000,40001',
      sites: [{ id: 4, label: 'Metro East', order: 1, siteRef: 101 }],
      talkgroups: [
        { id: 1, label: 'Dispatch 1', name: 'Dispatch 1', talkgroupRef: 1514000, order: 1, groupIds: [1], tagId: 2 },
        { id: 2, label: 'Dispatch 2', name: 'Dispatch 2', talkgroupRef: 1514100, order: 2, groupIds: [1], tagId: 2 },
      ],
      units: [{ id: 7, label: 'Car 1', order: 1, unitRef: 1001 }],
    },
  ];
}

function seed() {
  const { store } = getAdminContext();

  store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    'section:systems': { baseline: structuredClone(systems()), working: structuredClone(systems()), dirty: false, invalid: false, conflict: false },
    'section:groups': { baseline: [{ id: 1, label: 'Dispatch' }], working: [{ id: 1, label: 'Dispatch' }], dirty: false, invalid: false, conflict: false },
    'section:tags': { baseline: [{ id: 2, label: 'Law' }], working: [{ id: 2, label: 'Law' }], dirty: false, invalid: false, conflict: false },
    alerts: {},
  });

  return store;
}

function systemsSection() {
  const el = new (customElements.get('rdio-admin-systems'))();

  el.connectedCallback();

  return el;
}

// The field the list of entries belongs to.
function blacklistsControl(el) {
  return findAll(el, (node) => node.tag === 'textarea')[0];
}

// The entry rows the section renders beside the field: one per reference the list names.
function entryRows(el) {
  return findAll(el, (node) => node.className?.split(/\s+/).includes('rdio-admin-blacklist-entry'));
}

// What an entry row says out loud: the talkgroup it refuses, or the reference when the system has
// no talkgroup for it. The row also carries its Remove control, which is not part of what the entry
// names, so the reading takes the name the row gives the entry.
function entryText(entry) {
  return findAll(entry, (node) => node.className?.split(/\s+/).includes('rdio-admin-blacklist-entry-label'))[0].textContent.trim();
}

function removeControl(entry) {
  return findAll(entry, (node) => node.tag === 'button' && node.textContent === 'Remove')[0];
}

function saveButton(el) {
  const bar = findAll(el, (node) => node.tag === 'rdio-admin-section-bar')[0];

  bar.connectedCallback();

  return findAll(bar, (node) => node.tag === 'button' && node.className.split(/\s+/).includes('save'))[0];
}

function settled() {
  return new Promise((resolve) => setImmediate(resolve));
}

test('the systems section names each blacklisted talkgroup beside the field', () => {
  seed();

  const el = systemsSection();

  const entries = entryRows(el);

  assert.ok(entries.length === 2, `the section names ${entries.length} blacklisted entries, want one row per reference the list names`);

  assert.deepEqual(entries.map(entryText), ['Dispatch 1', '40001'], 'the entries are not named by the talkgroup they refuse, falling back to the reference the system has no talkgroup for');
});

test('removing an entry shortens the list to the entries that remain', async () => {
  const store = seed();

  const el = systemsSection();

  const [entry] = entryRows(el);

  assert.ok(entry, 'the systems section renders no entry row to remove');

  removeControl(entry).click();

  assert.equal(blacklistsControl(el).value, '40001', 'the field does not hold the entries that remain');

  assert.deepEqual(entryRows(el).map(entryText), ['40001'], 'the entry list did not follow the field');

  const slice = store.getState()['section:systems'];

  assert.equal(slice.dirty, true, 'removing an entry left the section not asking to be saved');
  assert.equal(slice.invalid, false, 'removing an entry made the section unsaveable');

  saveButton(el).click();

  await settled();

  const saved = requests.filter((request) => request.url === '/api/admin/config/systems');

  assert.equal(saved.length, 1, 'the section did not save the shortened list');

  assert.equal(JSON.parse(saved[0].options.body)[0].blacklists, '40001', 'the shortened list the section saved is not the list the operator was shown');
});
