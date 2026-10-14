// Blacklisting a talkgroup while the Systems section is asking to be saved. The system's blacklist is
// one field of the system row, and the Systems section saves every row it holds, so the list the
// section is holding is the list the server ends up storing. When the Blacklist action stores an entry
// for a system whose section is mid-edit, the section has to end up holding the list the server holds:
// a section that keeps the text it was showing writes that text back over the entry the operator just
// asked for, and the talkgroup row it was blacklisted from is already gone.
//
// The rows are laid out the way the conformance fixture lays them out: the first talkgroup of the
// first system is row 1 and reference 1514000.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-systems.js';

import { findAll, keystroke, reachField } from './dom-shim.js';

// The page reaches the server through fetch. Recording what fetch is asked to send shows the requests
// the Blacklist action and the Systems section's Save make, through the page's own api adapter, with no
// server in the test. The answers are the answers the server gives the same requests: the level endpoint
// answers the rows it stored beside the system's blacklist, and the section endpoint answers the systems
// as the store holds them, nested levels included.
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

// The system row of the Systems section, as the section holds it: the operator's field, saved by the
// section's own Save control.
function systemsRow(store, systemId) {
  return (store.getState()['section:systems']?.working ?? []).find((system) => system.id === systemId) ?? null;
}

function settled() {
  return new Promise((resolve) => setImmediate(resolve));
}

function requestTo(url) {
  return requests.filter((entry) => entry.url === url);
}

// The control of one field of one row of the Systems section, the way the operator reaches it: the
// row's field carrying that name, and the control inside it.
function fieldControl(el, rowIndex, title) {
  const row = el.querySelectorAll('.rdio-admin-row')[rowIndex];
  const field = [...row.querySelectorAll('rdio-form-field')].find((f) => f.querySelector('label')?.textContent === title);

  return field?.querySelector('input, select, textarea') ?? null;
}

// The operator's sequence: a field of the system row is edited and not saved, a talkgroup of that
// system is blacklisted, and the Systems section is saved to finish the edit. The edit is one the
// section marks as its own — the system's label — so it is not the blacklist field. The reading taken
// after the click and before the section's Save is the state the click leaves the section in.
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
