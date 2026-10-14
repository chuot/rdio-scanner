// The blacklist the operator edits. The field holds a comma separated list of talkgroup references,
// and it is the only place the operator edits that list: the section writes what the field holds when
// the system is saved, and nothing else takes a reference out. A reference the system has no talkgroup
// for is blacklisted the same way as one it has — the server compares the reference.
//
// The fixture lays the system out the way the conformance fixture does: the first talkgroup of the
// first system is reference 1514000, and the blacklist names that reference beside one the system has
// no talkgroup for.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-systems.js';
import '../presentation/admin/rdio-admin-talkgroups.js';

import { findAll } from './dom-shim.js';

// The page reaches the server through fetch. Recording what fetch is asked to send shows the body a
// save of the section writes, and the answer echoes back what it was given. The talkgroups level
// answers the rows it stored, the way the level endpoint answers them.
const requests = [];

globalThis.fetch = async (url, options = {}) => {
  requests.push({ url, options });

  const submitted = options.body ? JSON.parse(options.body) : {};

  const answer = url.endsWith('/talkgroups') && !Array.isArray(submitted) ? submitted.talkgroups : submitted;

  return {
    ok: true,
    status: 200,
    json: async () => answer,
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

// The field the operator edits the blacklist in.
function blacklistsControl(el) {
  return findAll(el, (node) => node.tag === 'textarea')[0];
}

// The rows a per-reference editor would render, and the control it would take a reference out with.
// Neither belongs on the page: the field is the editor.
function entryRows(el) {
  return findAll(el, (node) => node.className?.split(/\s+/).includes('rdio-admin-blacklist-entry'));
}

function removeControls(el) {
  return findAll(el, (node) => node.tag === 'button' && node.textContent === 'Remove');
}

function settled() {
  return new Promise((resolve) => setImmediate(resolve));
}

test('the systems section edits the blacklist through the field alone', () => {
  seed();

  const el = systemsSection();

  assert.equal(blacklistsControl(el).value, '1514000,40001',
    'the field does not hold the list the server stores, references the system has talkgroups for and one it has none for alike');

  assert.equal(entryRows(el).length, 0,
    'the section renders a line beside the field, where the operator was told the field is the editor');

  assert.equal(removeControls(el).length, 0,
    'the section renders a control that takes a reference out of the list, where the field is the editor');
});

// The reading the operator's report is made of. The system's blacklist starts empty, the operator
// blacklists a talkgroup in the Talkgroups section, and then opens the Systems section to look at it.
// The section is built after the action, which is the operator opening it after the click, with the
// page never reloaded.
function storedSystem() {
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
        { id: 1, label: 'Dispatch 1', name: 'Dispatch 1', talkgroupRef: 1514000, order: 1, groupIds: [1], tagId: 2 },
        { id: 2, label: 'Dispatch 2', name: 'Dispatch 2', talkgroupRef: 1514100, order: 2, groupIds: [1], tagId: 2 },
      ],
      units: [{ id: 7, label: 'Car 1', order: 1, unitRef: 1001 }],
    },
  ];
}

function seedStoredSystem() {
  const { store } = getAdminContext();

  const system = storedSystem()[0];

  const state = { auth: { authenticated: true, docker: false, passwordNeedChange: false } };

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

  store.setState({
    ...state,
    'section:systems': { baseline: structuredClone(storedSystem()), working: structuredClone(storedSystem()), dirty: false, invalid: false, conflict: false },
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

test('the systems section shows the blacklist entry the talkgroups action stored, with the page never reloaded', async () => {
  requests.length = 0;

  seedStoredSystem();

  const talkgroups = talkgroupsSection();

  blacklistButton(talkgroups).click();

  await settled();

  const systems = systemsSection();

  assert.equal(blacklistsControl(systems).value, '1514000',
    'the systems section does not show the blacklist entry the talkgroups action stored');
});
