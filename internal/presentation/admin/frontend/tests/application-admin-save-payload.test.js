// The payload a section Save sends. The rows the operator added and has not saved carry a mark of
// their own, which the section uses to remember the row's open panel and the fields whose messages are
// revealed. That mark is the client's bookkeeping: the payload carries the keys the configuration
// defines, and nothing the client added to keep its screens in step.
//
// The fake records the body the way the transport writes it — a JSON stringify of the payload — so the
// reading is the body the server receives, and the fake answers the way the server does: a row that
// arrived with `id: 0` comes back with an identifier of its own.

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

test('the systems save sends the whole array with its nested sub-lists, and no client key in any row', async () => {
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
      sites: [],
      systemRef: null,
      talkgroups: [],
      type: '',
      units: [],
    },
    COUNTY,
  ], 'the systems array the section saves is the configuration, nested sub-lists included');
});

// The store compares the working copy with the server's copy structurally. The client's mark is not a
// value the operator changed: a row that carries the mark and nothing else is the row the server holds.
test('the mark alone does not make a section dirty', async () => {
  const { store, useCases } = await setup();

  const baseline = store.getState()['section:groups'].baseline;

  useCases.setSectionWorking('groups', baseline.map((row) => markRowIdentity({ ...row })));

  const slice = store.getState()['section:groups'];

  assert.equal(slice.dirty, false, 'a working copy that differs from the server copy by the mark alone is not a change');
});

// The row the server saved is remembered by the identifier the server gave it: the client's mark is
// gone from the section's rows with the save, and the row keeps its place in the section's readings.
test('a saved row is remembered by the identifier the server gave it', async () => {
  const { store, useCases } = await setup();

  useCases.setSectionWorking('groups', withNewRow(CONFIG.groups, { alert: '', id: 0, label: 'Law', led: '', order: 0 }));

  assert.equal(await useCases.saveSection('groups'), true);

  const slice = store.getState()['section:groups'];

  assert.equal(slice.dirty, false, 'the section is clean after its save');
  assert.equal(rowIdentity(slice.working[0]), null, 'the saved row carries no client mark');
  assert.equal(rowKey(slice.working[0], 0), 'id7', 'the row is remembered by the identifier the server gave it');
});

// The mask the add control creates is the row's own configuration key: it rides into the body the
// server reads, and the row the save answers carries the mask the section's rule accepts — the row
// means the same thing on both sides of its save.
test('the dirwatch row the add control creates is saved carrying its mask', async () => {
  const { api, useCases } = await setup();

  useCases.setSectionWorking('dirwatch', withNewRow(CONFIG.dirwatch, { delay: 2000, deleteAfter: true, directory: '/var/spool/payload', disabled: false, mask: '#SYS_#TG', order: 0, type: 'default' }));

  assert.equal(await useCases.saveSection('dirwatch'), true);

  const body = api.calls.find((call) => call[0] === 'saveSection')[2];

  assert.deepEqual(body, [
    { delay: 2000, deleteAfter: true, directory: '/var/spool/payload', disabled: false, mask: '#SYS_#TG', order: 0, type: 'default' },
  ], 'the body carries the mask among the configuration keys the row owns, and no key the client added');
});
