// the save payload carries the configuration's keys, not the client's row mark
// the fake stores the stringified body and answers like the server: id 0 gets an id

import assert from 'node:assert/strict';
import test from 'node:test';

import { createAdminUseCases } from '../application/admin-use-cases.js';
import { createStore } from '../../../common/frontend/application/store.js';
import { markRowIdentity, rowIdentity, rowKey } from '../presentation/admin/row-builders.js';

const COUNTY = {
  alert: '',
  autoPopulate: false,
  blacklists: '',
  delay: null,
  id: 2,
  label: 'County',
  led: '',
  order: 1,
  sites: [{ id: 4, label: 'East', order: 1, siteRef: 40 }],
  systemRef: 2,
  talkgroups: [],
  type: '',
  units: [],
};

const CONFIG = {
  access: [],
  apikeys: [],
  dirwatch: [],
  downstreams: [],
  groups: [{ alert: '', id: 1, label: 'Dispatch', led: '', order: 1 }],
  options: { branding: 'X' },
  systems: [COUNTY],
  tags: [],
};

function fakeStorage() {
  let stored = 'jwt-0';

  return { getAdminToken: () => stored, setAdminToken() {}, clearAdminToken() { stored = null; } };
}

function fakeApi() {
  const calls = [];
  let nextServerId = 7;

  // The transport is a JSON stringify: the body the server reads is the payload as JSON writes it.
  const asBody = (value) => JSON.parse(JSON.stringify(value));
  const saved = (rows) => rows.map((row) => (row.id === 0 ? { ...row, id: nextServerId++ } : row));

  return {
    calls,
    async getConfig() {
      return { config: structuredClone(CONFIG), docker: false, passwordNeedChange: false };
    },
    async getAlerts() {
      return {};
    },
    async getSection(section) {
      calls.push(['getSection', section]);

      return structuredClone(CONFIG[section] ?? null);
    },
    async saveSection(section, value) {
      const body = asBody(value);

      calls.push(['saveSection', section, body]);

      return saved(body);
    },
    async getSubSection(systemId, level) {
      calls.push(['getSubSection', systemId, level]);

      return structuredClone((CONFIG.systems.find((system) => system.id === systemId) ?? {})[level] ?? null);
    },
    async saveSubSection(systemId, level, value) {
      const body = asBody(value);

      calls.push(['saveSubSection', systemId, level, body]);

      return saved(body);
    },
  };
}

async function setup() {
  const api = fakeApi();
  const store = createStore({ auth: { authenticated: false, docker: false, passwordNeedChange: false }, todos: [] });
  const useCases = createAdminUseCases({
    api,
    storage: fakeStorage(),
    createSocket: () => ({ connect() {}, close() {}, subscribe: () => () => {} }),
    store,
  });

  await useCases.boot();

  return { api, store, useCases };
}

// The row the operator added, carrying the mark the section gives it, beside the row the server holds.
function withNewRow(rows, fresh) {
  return [markRowIdentity(fresh), ...rows];
}

test('a section save sends the rows with the keys the configuration defines, and nothing the client added', async () => {
  const { api, useCases } = await setup();

  useCases.setSectionWorking('groups', withNewRow(CONFIG.groups, { alert: '', id: 0, label: '', led: '', order: 0 }));

  assert.equal(await useCases.saveSection('groups'), true);

  const body = api.calls.find((call) => call[0] === 'saveSection')[2];

  assert.deepEqual(body, [
    { alert: '', id: 0, label: '', led: '', order: 0 },
    { alert: '', id: 1, label: 'Dispatch', led: '', order: 1 },
  ], 'the body carries the configuration keys, and no key the client added to remember the row');
});

test('a per-level save sends the rows with the keys the configuration defines', async () => {
  const { api, useCases } = await setup();

  useCases.setSubSectionWorking('sites', COUNTY.id, withNewRow(COUNTY.sites, { id: 0, label: 'Depot', order: 0, siteRef: 41 }));

  assert.equal(await useCases.saveSubSection('sites', COUNTY.id), true);

  const body = api.calls.find((call) => call[0] === 'saveSubSection')[3];

  assert.deepEqual(body, [
    { id: 0, label: 'Depot', order: 0, siteRef: 41 },
    { id: 4, label: 'East', order: 1, siteRef: 40 },
  ], 'the level of the system the operator saved carries its configuration keys and nothing else');
});

// the systems save carries no copy of the sites, talkgroups, and units
test('the systems save sends the fields of the system row, and no copy of the levels', async () => {
  const { api, useCases } = await setup();

  useCases.setSectionWorking('systems', withNewRow([COUNTY], {
    alert: '',
    autoPopulate: false,
    blacklists: '',
    delay: null,
    id: 0,
    label: 'West',
    led: '',
    order: 0,
    sites: [],
    systemRef: null,
    talkgroups: [],
    type: '',
    units: [],
  }));

  assert.equal(await useCases.saveSection('systems'), true);

  const body = api.calls.find((call) => call[0] === 'saveSection')[2];

  assert.deepEqual(body, [
    {
      alert: '',
      autoPopulate: false,
      blacklists: '',
      delay: null,
      id: 0,
      label: 'West',
      led: '',
      order: 0,
      systemRef: null,
      type: '',
    },
    {
      alert: '',
      autoPopulate: false,
      blacklists: '',
      delay: null,
      id: 2,
      label: 'County',
      led: '',
      order: 1,
      systemRef: 2,
      type: '',
    },
  ], 'the systems save sends the fields of the system row, and no copy of the sites, talkgroups, and units the level sections save');
});

// stored sites, talkgroups, and units stay the level sections' to write
test('the systems save leaves the stored sites, talkgroups, and units to the sections that save them', async () => {
  const { api, useCases } = await setup();

  useCases.setSectionWorking('systems', [COUNTY, { ...COUNTY, id: 3, label: 'River', systemRef: 3 }]);

  assert.equal(await useCases.saveSection('systems'), true);

  const body = api.calls.find((call) => call[0] === 'saveSection')[2];

  for (const entry of body) {
    for (const level of ['sites', 'talkgroups', 'units']) {
      assert.equal(entry[level], undefined, `the systems save sends ${level}, which the ${level} section saves for the system it shows`);
    }
  }
});

// the working copy differing from the server copy by the mark alone is not dirty
test('the mark alone does not make a section dirty', async () => {
  const { store, useCases } = await setup();

  const baseline = store.getState()['section:groups'].baseline;

  useCases.setSectionWorking('groups', baseline.map((row) => markRowIdentity({ ...row })));

  const slice = store.getState()['section:groups'];

  assert.equal(slice.dirty, false, 'a working copy that differs from the server copy by the mark alone is not a change');
});

// a saved row loses its client mark and is keyed by the server's identifier
test('a saved row is remembered by the identifier the server gave it', async () => {
  const { store, useCases } = await setup();

  useCases.setSectionWorking('groups', withNewRow(CONFIG.groups, { alert: '', id: 0, label: 'Law', led: '', order: 0 }));

  assert.equal(await useCases.saveSection('groups'), true);

  const slice = store.getState()['section:groups'];

  assert.equal(slice.dirty, false, 'the section is clean after its save');
  assert.equal(rowIdentity(slice.working[0]), null, 'the saved row carries no client mark');
  assert.equal(rowKey(slice.working[0], 0), 'id7', 'the row is remembered by the identifier the server gave it');
});

// the dirwatch mask the add control creates rides into the saved body, rule-clean
test('the dirwatch row the add control creates is saved carrying its mask', async () => {
  const { api, useCases } = await setup();

  useCases.setSectionWorking('dirwatch', withNewRow(CONFIG.dirwatch, { delay: 2000, deleteAfter: true, directory: '/var/spool/payload', disabled: false, mask: '#SYS_#TG', order: 0, type: 'default' }));

  assert.equal(await useCases.saveSection('dirwatch'), true);

  const body = api.calls.find((call) => call[0] === 'saveSection')[2];

  assert.deepEqual(body, [
    { delay: 2000, deleteAfter: true, directory: '/var/spool/payload', disabled: false, mask: '#SYS_#TG', order: 0, type: 'default' },
  ], 'the body carries the mask among the configuration keys the row owns, and no key the client added');
});
