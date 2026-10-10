// The per-level store slices (T017, CA-004, CA-005, CA-007): each level of each
// system — sites, talkgroups, units — lives in a `subsection:<level>:<systemId>`
// slice with the same baseline/working/dirty/invalid/conflict semantics as the
// section slices, saves through the per-level endpoints, and keeps the talkgroup
// order-key normalization the systems section does.

import test from 'node:test';
import assert from 'node:assert/strict';

import { createAdminUseCases } from '../application/admin-use-cases.js';
import { createStore } from '../../../common/frontend/application/store.js';

function storedConfig() {
  return {
    groups: [{ id: 1, label: 'Dispatch' }],
    tags: [{ id: 2, label: 'Tag' }],
    systems: [
      {
        id: 1,
        label: 'Metro',
        order: 1,
        sites: [{ id: 5, label: 'East', siteRef: 77, order: 1 }],
        talkgroups: [{ id: 10, label: 'Ground', talkgroupRef: 1514, talkgroup: 5, groupIds: [1], tagId: 2 }],
        units: [{ id: 20, label: 'Car 1', unitRef: 1001, order: 1 }],
      },
      {
        id: 2,
        label: 'Harbour',
        order: 2,
        sites: [],
        talkgroups: [],
        units: [],
      },
    ],
  };
}

function fakeApi() {
  const calls = [];

  return {
    calls,

    async getConfig() {
      return { config: structuredClone(storedConfig()), docker: false, passwordNeedChange: false };
    },

    async getAlerts() {
      return {};
    },

    async getSubSection(systemId, level) {
      calls.push(['getSubSection', systemId, level]);

      const system = storedConfig().systems.find((s) => s.id === systemId);

      return structuredClone(system[level]);
    },

    async saveSubSection(systemId, level, value) {
      calls.push(['saveSubSection', systemId, level, structuredClone(value)]);

      return structuredClone(value);
    },
  };
}

function fakeSocket() {
  let listener = null;

  return {
    subscribe(cb) {
      listener = cb;
    },
    connect() {},
    close() {},
    push(event) {
      listener(event);
    },
  };
}

function setup() {
  const store = createStore({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    config: null,
    todos: [],
    alerts: {},
  });

  const api = fakeApi();
  const socket = fakeSocket();

  const useCases = createAdminUseCases({
    api,
    storage: { getAdminToken: () => 'jwt-1', setAdminToken() {}, clearAdminToken() {} },
    createSocket: () => socket,
    store,
  });

  return { store, api, socket, useCases };
}

test('the config load gives every level of every system its own slice', async () => {
  const { store, useCases } = setup();

  await useCases.loadConfig();

  for (const key of [
    'subsection:sites:1',
    'subsection:talkgroups:1',
    'subsection:units:1',
    'subsection:sites:2',
    'subsection:talkgroups:2',
    'subsection:units:2',
  ]) {
    const slice = store.getState()[key];

    assert.ok(slice, `the store has no slice for ${key}`);
    assert.equal(slice.dirty, false);
    assert.equal(slice.conflict, false);
  }

  const sites = store.getState()['subsection:sites:1'];

  assert.equal(sites.baseline[0].label, 'East');
  assert.equal(sites.working[0].label, 'East');

  // The talkgroup order key the server emits is normalized to order, as the
  // systems section does.
  const talkgroups = store.getState()['subsection:talkgroups:1'];

  assert.equal(talkgroups.working[0].order, 5);
  assert.equal(talkgroups.working[0].talkgroup, undefined);
});

test('editing a level marks its slice dirty and the save issues the per-level PUT only', async () => {
  const { store, api, useCases } = setup();

  await useCases.loadConfig();

  const rows = [{ id: 5, label: 'Renamed', siteRef: 77, order: 1 }, { label: 'New', siteRef: 102, order: 2 }];

  useCases.setSubSectionWorking('sites', 1, rows);

  assert.equal(store.getState()['subsection:sites:1'].dirty, true);

  const saved = await useCases.saveSubSection('sites', 1);

  assert.equal(saved, true);

  const call = api.calls.find(([name]) => name === 'saveSubSection');

  assert.deepEqual(call, ['saveSubSection', 1, 'sites', rows]);

  const slice = store.getState()['subsection:sites:1'];

  assert.equal(slice.dirty, false);
  assert.equal(slice.baseline[0].label, 'Renamed');

  // The config the socket and the systems section read carries the saved rows.
  assert.equal(store.getState().config.systems[0].sites[0].label, 'Renamed');
});

test('the talkgroup save serializes the order key the server reads', async () => {
  const { store, api, useCases } = setup();

  await useCases.loadConfig();

  useCases.setSubSectionWorking('talkgroups', 1, [{ id: 10, label: 'Ground', talkgroupRef: 1514, order: 5, groupIds: [1], tagId: 2 }]);

  await useCases.saveSubSection('talkgroups', 1);

  const call = api.calls.find(([name]) => name === 'saveSubSection');

  const saved = call[3][0];

  assert.equal(saved.order, 5);
  assert.equal(saved.talkgroup, undefined);
});

test('a level can be discarded and reloaded from the server', async () => {
  const { store, api, useCases } = setup();

  await useCases.loadConfig();

  useCases.setSubSectionWorking('units', 1, [{ label: 'Edited', unitRef: 1, order: 1 }]);

  useCases.discardSubSection('units', 1);

  const slice = store.getState()['subsection:units:1'];

  assert.equal(slice.dirty, false);
  assert.equal(slice.working[0].label, 'Car 1');

  useCases.setSubSectionWorking('units', 1, [{ label: 'Edited', unitRef: 1, order: 1 }]);

  const reloaded = await useCases.reloadSubSection('units', 1);

  assert.equal(reloaded, true);
  assert.equal(store.getState()['subsection:units:1'].working[0].label, 'Car 1');

  assert.deepEqual(api.calls.find(([name]) => name === 'getSubSection'), ['getSubSection', 1, 'units']);
});

test('a server push on a dirty level flags the conflict', async () => {
  const { store, socket, useCases } = setup();

  await useCases.boot();

  useCases.setSubSectionWorking('sites', 1, [{ id: 5, label: 'Renamed', siteRef: 77, order: 1 }]);

  socket.push({ type: 'config', config: storedConfig() });

  const slice = store.getState()['subsection:sites:1'];

  assert.equal(slice.conflict, true);
  assert.equal(slice.working[0].label, 'Renamed', 'the local edit survives the push');
});
