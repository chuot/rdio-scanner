// the blacklist is one field of the system row: the section saves what it holds
// after a Blacklist stores an entry, the section must adopt it or overwrite it.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-systems.js';

import { findAll, keystroke, reachField } from './dom-shim.js';

// the test records both requests through the page's api adapter; no
// server.
const requests = [];

globalThis.fetch = async (url, options = {}) => {
  requests.push({ url, options });

  const submitted = options.body ? JSON.parse(options.body) : {};

  if (url.endsWith('/talkgroups')) {
    return { ok: true, status: 200, json: async () => (Array.isArray(submitted) ? submitted : submitted.talkgroups) };
  }

  if (url === '/api/admin/config/systems' && Array.isArray(submitted)) {
    return {
      ok: true,
      status: 200,
      json: async () => systems().map((system) => {
        const row = submitted.find((submittedRow) => submittedRow.id === system.id) ?? {};

        return { ...system, ...row };
      }),
    };
  }

  return { ok: true, status: 200, json: async () => submitted };
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
        { id: 1, label: 'Dispatch 1', name: 'Dispatch 1', talkgroupRef: 1514000, order: 1, groupIds: [1], tagId: 2 },
        { id: 2, label: 'Dispatch 2', name: 'Dispatch 2', talkgroupRef: 1514100, order: 2, groupIds: [1], tagId: 2 },
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

// the Systems section's system row: the operator's field, saved by its Save
function systemsRow(store, systemId) {
  return (store.getState()['section:systems']?.working ?? []).find((system) => system.id === systemId) ?? null;
}

function settled() {
  return new Promise((resolve) => setImmediate(resolve));
}

function requestTo(url) {
  return requests.filter((entry) => entry.url === url);
}

function snackbarText() {
  return document.querySelector('rdio-snackbar')?.textContent ?? '';
}

// the save bar is its own component: Save and Discard, like the page's bar
function barControl(el, name) {
  const bar = findAll(el, (node) => node.tag === 'rdio-admin-section-bar')[0];

  bar.connectedCallback();

  return findAll(bar, (node) => node.tag === 'button' && node.className.split(/\s+/).includes(name))[0];
}

// one field of one row as the operator reaches it: the field name, inner control
function fieldControl(el, rowIndex, title) {
  const row = el.querySelectorAll('.rdio-admin-row')[rowIndex];
  const field = [...row.querySelectorAll('rdio-form-field')].find((f) => f.querySelector('label')?.textContent === title);

  return field?.querySelector('input, select, textarea') ?? null;
}

// the sequence: edit a system field unsaved, blacklist one of its talkgroups
// then save the Systems section; the reading before Save is the click's state
async function editASystemFieldThenBlacklist({ saveSystems = true } = {}) {
  requests.length = 0;

  const store = seed();

  const systemsSection = new (customElements.get('rdio-admin-systems'))();

  systemsSection.connectedCallback();

  const label = fieldControl(systemsSection, 0, 'Label');

  assert.ok(label, 'the Systems section renders no Label control for the system row');

  keystroke(label, 'Grand Metro renamed');
  reachField(label);

  const el = talkgroupsSection();

  blacklistButton(el).click();

  await settled();

  const afterClick = {
    blacklists: systemsRow(store, 1)?.blacklists ?? null,
    dirty: store.getState()['section:systems']?.dirty ?? null,
  };

  let saved = null;

  if (saveSystems) {
    saved = await getAdminContext().useCases.saveSection('systems');

    await settled();
  }

  return { store, saved, afterClick, systemsSection };
}

// if the only pending edit was the blacklist field, storing it clears dirtiness
test('a Systems section whose only pending edit was the blacklist field stops asking to be saved when the Blacklist action stores its entry', async () => {
  requests.length = 0;

  const store = seed();

  const systemsSection = new (customElements.get('rdio-admin-systems'))();

  systemsSection.connectedCallback();

  const field = fieldControl(systemsSection, 0, 'Blacklists');

  assert.ok(field, 'the Systems section renders no Blacklists control for the system row');

  keystroke(field, '40001');
  reachField(field);

  assert.equal(store.getState()['section:systems'].dirty, true, 'the Systems section did not mark the draft of its blacklist field as unsaved');

  blacklistButton(talkgroupsSection()).click();

  await settled();

  const section = store.getState()['section:systems'];

  assert.equal(section.dirty, false, 'the Systems section is still asking to be saved after the Blacklist action stored the list it holds');
  assert.equal(systemsRow(store, 1)?.blacklists, '1514000', 'the Blacklist action left the section holding the draft the operator typed, not the list the server stored');
});

// during another pending edit the field shows the stored list
// and names the blacklisted talkgroup by its reference
test('the Systems section shows the blacklisted talkgroup after the Blacklist action, while it is asking to be saved for another edit', async () => {
  requests.length = 0;

  const store = seed();

  const systemsSection = new (customElements.get('rdio-admin-systems'))();

  systemsSection.connectedCallback();

  keystroke(fieldControl(systemsSection, 0, 'Label'), 'Grand Metro renamed');
  reachField(fieldControl(systemsSection, 0, 'Label'));

  blacklistButton(talkgroupsSection()).click();

  await settled();

  assert.equal(store.getState()['section:systems'].dirty, true, 'the Blacklist action stopped the Systems section asking to be saved while the operator had an edit pending in it');

  assert.equal(fieldControl(systemsSection, 0, 'Blacklists')?.value, '1514000', 'the Blacklists field the operator reads after the Blacklist action is not the list the server stored');
});

// the click replaces an unsaved Blacklists draft and says what it replaced
test('the Blacklist action replaces an unsaved draft of the Blacklists field with the list it stores, and says so', async () => {
  requests.length = 0;

  const store = seed();

  const systemsSection = new (customElements.get('rdio-admin-systems'))();

  systemsSection.connectedCallback();

  keystroke(fieldControl(systemsSection, 0, 'Blacklists'), '40001');
  reachField(fieldControl(systemsSection, 0, 'Blacklists'));

  blacklistButton(talkgroupsSection()).click();

  await settled();

  const message = snackbarText();

  assert.ok(message.includes('Blacklists'), `the Blacklist action replaced the operator's draft of the field without saying so, it said "${message}"`);

  assert.equal(fieldControl(systemsSection, 0, 'Blacklists')?.value, '1514000', 'the field the operator reads after the Blacklist action is not the list the server stored');

  const saved = await getAdminContext().useCases.saveSection('systems');

  await settled();

  assert.equal(saved, true, 'the Systems section refused to save');

  const body = JSON.parse(requestTo('/api/admin/config/systems').at(-1).options.body);

  assert.equal(body.find((system) => system.id === 1)?.blacklists, '1514000', 'the Systems section saved the draft the operator had typed, over the list the Blacklist action stored');

  assert.equal(store.getState()['blacklists:1'].working, '1514000', 'the list the page holds after the Systems section saved is not the list the server holds');
});

// Discard retracts the section's own edit only
// the stored blacklist is the server's, not part of the edit
test('Discard in the Systems section, after the Blacklist action, leaves the field holding the list the server stored', async () => {
  requests.length = 0;

  const store = seed();

  const systemsSection = new (customElements.get('rdio-admin-systems'))();

  systemsSection.connectedCallback();

  keystroke(fieldControl(systemsSection, 0, 'Label'), 'Grand Metro renamed');
  reachField(fieldControl(systemsSection, 0, 'Label'));

  blacklistButton(talkgroupsSection()).click();

  await settled();

  const discard = barControl(systemsSection, 'discard');

  assert.ok(discard && !discard.disabled, 'the Systems section is not offering to discard the edit the operator made');

  discard.click();

  await settled();

  const section = store.getState()['section:systems'];

  assert.equal(section.dirty, false, 'the Systems section is asking to be saved after the operator discarded their edit');
  assert.equal(systemsRow(store, 1)?.blacklists, '1514000', 'Discard in the Systems section took back the entry the Blacklist action stored');
  assert.equal(fieldControl(systemsSection, 0, 'Blacklists')?.value, '1514000', 'the field the operator reads after Discard is not the list the server holds');
});

test('the Systems section saves the list the Blacklist action stored, when the section was asking to be saved for another field', async () => {
  const { store, saved, afterClick } = await editASystemFieldThenBlacklist();

  assert.equal(
    afterClick.blacklists,
    '1514000',
    'the Blacklist action stored its entry while the Systems section was holding the list it showed before the click',
  );

  assert.equal(afterClick.dirty, true, 'the Blacklist action stopped the Systems section asking to be saved while the operator had an edit pending in it');

  assert.equal(saved, true, 'the Systems section refused to save the edit the operator made');

  const systemsSave = requestTo('/api/admin/config/systems').at(-1);

  assert.ok(systemsSave, 'the Systems section saved without asking the server');

  const body = JSON.parse(systemsSave.options.body);

  assert.equal(
    body.find((system) => system.id === 1)?.blacklists,
    '1514000',
    'the Systems section saved the list it was showing before the Blacklist action, over the entry the action stored',
  );

  assert.equal(body.find((system) => system.id === 1)?.label, 'Grand Metro renamed', 'the Systems section lost the edit the operator made');

  assert.equal(store.getState()['section:systems'].dirty, false, 'the saved Systems section is still asking to be saved');
});
