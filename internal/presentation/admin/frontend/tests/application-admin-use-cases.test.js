// Admin login/logout orchestration (application/admin-use-cases.js).
// Red first: these tests define the boot/login/logout/session-expiry contract.

import test from 'node:test';
import assert from 'node:assert/strict';

import { createAdminUseCases } from '../application/admin-use-cases.js';
import { createStore } from '../../../common/frontend/application/store.js';

function fakeStorage(token = null) {
  let stored = token;

  return {
    calls: [],
    getAdminToken: () => stored,
    setAdminToken(t) {
      this.calls.push(['set', t]);
      stored = t;
    },
    clearAdminToken() {
      this.calls.push(['clear', null]);
      stored = null;
    },
  };
}

const SECTIONS = ['access', 'apikeys', 'dirwatch', 'downstreams', 'groups', 'options', 'systems', 'tags'];

function sectionConfig() {
  return {
    access: [],
    apikeys: [],
    dirwatch: [],
    downstreams: [],
    groups: [{ id: 1, label: 'Dispatch' }],
    options: { branding: 'X' },
    systems: [{ id: 1, label: 'S', talkgroups: [{ id: 10, label: 'T', talkgroup: 5 }] }],
    tags: [],
  };
}

function fakeApi({ loginResult = { token: 'jwt-1', passwordNeedChange: true }, config = { config: sectionConfig(), passwordNeedChange: true }, fetchedSections = {}, saveError = false, changePasswordError = false, storage } = {}) {
  const calls = [];

  return {
    calls,
    async login(password) {
      calls.push(['login', password]);

      if (loginResult instanceof Error) throw loginResult;

      // mirrors infrastructure/admin-api.js: the adapter owns token storage.
      storage.setAdminToken(loginResult.token);

      return loginResult;
    },
    async logout() {
      calls.push(['logout']);
    },
    async getConfig() {
      calls.push(['getConfig']);

      if (config instanceof Error) throw config;

      return config;
    },
    async getSection(section) {
      calls.push(['getSection', section]);

      return structuredClone(fetchedSections[section] ?? null);
    },
    async saveSection(section, value) {
      calls.push(['saveSection', section, structuredClone(value)]);

      if (saveError) throw new Error('Admin API PUT /config/groups: 417');

      return structuredClone(value);
    },
    async changePassword(currentPassword, newPassword) {
      calls.push(['changePassword', currentPassword, newPassword]);

      if (changePasswordError) throw new Error('Admin API POST /password: 417');

      return { passwordNeedChange: false };
    },
  };
}

function fakeSockets() {
  const opened = [];

  return {
    opened,
    createSocket(token) {
      const socket = {
        token,
        closed: false,
        subscribers: new Set(),
        connect() {
          this.connected = true;
        },
        close() {
          this.closed = true;
        },
        subscribe(cb) {
          this.subscribers.add(cb);
          return () => this.subscribers.delete(cb);
        },
        emit(event) {
          for (const cb of this.subscribers) cb(event);
        },
      };

      opened.push(socket);

      return socket;
    },
  };
}

function setup(options = {}) {
  const storage = fakeStorage(options.token ?? null);
  const api = fakeApi({ ...options.apiOptions, storage });
  const sockets = fakeSockets();
  const store = createStore({
    auth: { authenticated: false, docker: false, passwordNeedChange: false },
    todos: [],
  });

  const useCases = createAdminUseCases({ api, storage, createSocket: sockets.createSocket, store });

  return { api, sockets, storage, store, useCases };
}

test('boot without a stored token shows the login screen and never opens the config socket', async () => {
  const { api, sockets, store, useCases } = setup();

  await useCases.boot();

  assert.equal(store.getState().auth.authenticated, false);
  assert.equal(sockets.opened.length, 0);
  assert.deepEqual(api.calls.map((c) => c[0]), []);
});

test('boot with a stored token validates it via GET config and opens the config socket', async () => {
  const { api, sockets, store, useCases } = setup({ token: 'jwt-0' });

  await useCases.boot();

  assert.deepEqual(api.calls.map((c) => c[0]), ['getConfig']);
  assert.equal(store.getState().auth.authenticated, true);
  assert.equal(sockets.opened.length, 1);
  assert.equal(sockets.opened[0].token, 'jwt-0');
  assert.equal(store.getState().auth.passwordNeedChange, true);
});

test('boot with a stale token (server restarted) ends the session', async () => {
  const { store, useCases } = setup({
    token: 'jwt-0',
    apiOptions: { config: new Error('Unauthorized') },
  });

  await useCases.boot();

  assert.equal(store.getState().auth.authenticated, false);
});

test('login stores the token, marks the session authenticated, and opens the config socket', async () => {
  const { api, sockets, storage, store, useCases } = setup();

  const ok = await useCases.login('secret');

  assert.equal(ok, 'ok');
  assert.deepEqual(api.calls[0], ['login', 'secret']);
  assert.deepEqual(storage.calls, [['set', 'jwt-1']]);
  assert.equal(store.getState().auth.authenticated, true);
  assert.equal(sockets.opened.length, 1);
  assert.equal(sockets.opened[0].token, 'jwt-1');
});

test('login failure (401 / rate limit) returns invalid and keeps the login screen', async () => {
  const { sockets, storage, store, useCases } = setup({
    apiOptions: { loginResult: new Error('Unauthorized') },
  });

  const ok = await useCases.login('wrong');

  assert.equal(ok, 'invalid');
  assert.equal(store.getState().auth.authenticated, false);
  assert.equal(storage.getAdminToken(), null);
  assert.equal(sockets.opened.length, 0);
});

test('login while the server is unreachable returns unreachable, not invalid', async () => {
  const { sockets, store, useCases } = setup({
    apiOptions: { loginResult: new TypeError('Failed to fetch') },
  });

  const ok = await useCases.login('secret');

  assert.equal(ok, 'unreachable');
  assert.equal(store.getState().auth.authenticated, false);
  assert.equal(sockets.opened.length, 0);
});

test('logout clears the token, closes the config socket, and returns to the login screen', async () => {
  const { api, sockets, storage, store, useCases } = setup({ token: 'jwt-0' });

  await useCases.boot();

  await useCases.logout();

  assert.deepEqual(api.calls.at(-1), ['logout']);
  assert.equal(storage.getAdminToken(), null);
  assert.equal(store.getState().auth.authenticated, false);
  assert.equal(sockets.opened[0].closed, true);
});

test('config socket close 1000 ends the session (server-side logout / restart)', async () => {
  const { sockets, storage, store, useCases } = setup({ token: 'jwt-0' });

  await useCases.boot();

  sockets.opened[0].emit({ type: 'unauthorized' });

  assert.equal(store.getState().auth.authenticated, false);
  assert.equal(storage.getAdminToken(), null);
  assert.equal(sockets.opened[0].closed, true);
});

test('a rejected 401 from any admin request ends the session', async () => {
  const storage = fakeStorage('jwt-0');
  const sockets = fakeSockets();
  const store = createStore({
    auth: { authenticated: false, docker: false, passwordNeedChange: false },
    todos: [],
  });

  let useCases;

  const api = {
    async login() {
      return { token: 'jwt-0', passwordNeedChange: false };
    },
    async logout() {},
    async getConfig() {
      storage.clearAdminToken();
      useCases.sessionExpired();

      throw new Error('Unauthorized');
    },
  };

  useCases = createAdminUseCases({ api, storage, createSocket: sockets.createSocket, store });

  await useCases.boot();

  assert.equal(store.getState().auth.authenticated, false);
});

// --- per-section baseline/working/dirty/conflict state ----------------------

test('boot initializes a slice for each of the 8 sections with independent working copies', async () => {
  const { store, useCases } = setup({ token: 'jwt-0' });

  await useCases.boot();

  for (const name of SECTIONS) {
    const slice = store.getState()[`section:${name}`];

    assert.ok(slice, `missing section slice: ${name}`);
    assert.equal(slice.dirty, false);
    assert.equal(slice.invalid, false);
    assert.equal(slice.conflict, false);
    assert.notEqual(slice.working, slice.baseline, `${name} working copy must be a deep copy`);
  }

  assert.deepEqual(store.getState()['section:groups'].working, [{ id: 1, label: 'Dispatch' }]);

  // round-trip normalization: server emits
  // talkgroup order under "talkgroup"; the working copy renames it to "order".
  assert.equal(store.getState()['section:systems'].working[0].talkgroups[0].order, 5);
  assert.equal('talkgroup' in store.getState()['section:systems'].working[0].talkgroups[0], false);
});

test('setSectionWorking marks the section dirty only when it differs from the baseline', async () => {
  const { store, useCases } = setup({ token: 'jwt-0' });

  await useCases.boot();

  useCases.setSectionWorking('groups', [{ id: 1, label: 'Renamed' }]);

  assert.equal(store.getState()['section:groups'].dirty, true);

  useCases.setSectionWorking('groups', [{ id: 1, label: 'Dispatch' }]);

  assert.equal(store.getState()['section:groups'].dirty, false);
});

test('a config push updates clean sections in place', async () => {
  const { sockets, store, useCases } = setup({ token: 'jwt-0' });

  await useCases.boot();

  sockets.opened[0].emit({ type: 'config', config: { ...sectionConfig(), groups: [{ id: 1, label: 'Server Renamed' }] } });

  const slice = store.getState()['section:groups'];

  assert.deepEqual(slice.baseline, [{ id: 1, label: 'Server Renamed' }]);
  assert.deepEqual(slice.working, [{ id: 1, label: 'Server Renamed' }]);
  assert.equal(slice.dirty, false);
  assert.equal(slice.conflict, false);
});

test('a config push while a section is dirty flags a conflict and preserves the working copy', async () => {
  const { sockets, store, useCases } = setup({ token: 'jwt-0' });

  await useCases.boot();

  useCases.setSectionWorking('groups', [{ id: 1, label: 'Mine' }]);

  sockets.opened[0].emit({ type: 'config', config: { ...sectionConfig(), groups: [{ id: 1, label: 'Theirs' }] } });

  const slice = store.getState()['section:groups'];

  assert.equal(slice.conflict, true);
  assert.deepEqual(slice.working, [{ id: 1, label: 'Mine' }]);
  assert.deepEqual(slice.baseline, [{ id: 1, label: 'Theirs' }]);
});

test('a push that matches the dirty working copy clears the conflict', async () => {
  const { sockets, store, useCases } = setup({ token: 'jwt-0' });

  await useCases.boot();

  useCases.setSectionWorking('groups', [{ id: 1, label: 'Same' }]);

  sockets.opened[0].emit({ type: 'config', config: { ...sectionConfig(), groups: [{ id: 1, label: 'Same' }] } });

  const slice = store.getState()['section:groups'];

  assert.equal(slice.dirty, false);
  assert.equal(slice.conflict, false);
});

test('saveSection sends the working copy, then re-baselines from the saved section', async () => {
  const { api, store, useCases } = setup({ token: 'jwt-0' });

  await useCases.boot();

  useCases.setSectionWorking('groups', [{ id: 1, label: 'Renamed' }]);

  const ok = await useCases.saveSection('groups');

  assert.equal(ok, true);

  const call = api.calls.find((c) => c[0] === 'saveSection');

  assert.deepEqual(call, ['saveSection', 'groups', [{ id: 1, label: 'Renamed' }]]);

  const slice = store.getState()['section:groups'];

  assert.equal(slice.dirty, false);
  assert.equal(slice.conflict, false);
  assert.deepEqual(slice.baseline, [{ id: 1, label: 'Renamed' }]);
  assert.deepEqual(slice.working, [{ id: 1, label: 'Renamed' }]);
});

test('saveSection serializes the systems section with the "order" key (round-trip contract)', async () => {
  const { api, useCases } = setup({ token: 'jwt-0' });

  await useCases.boot();

  useCases.setSectionWorking('systems', [{ id: 1, label: 'S', talkgroups: [{ id: 10, label: 'T', order: 7 }] }]);

  await useCases.saveSection('systems');

  const call = api.calls.find((c) => c[0] === 'saveSection');
  const talkgroup = call[2][0].talkgroups[0];

  assert.equal(talkgroup.order, 7);
  assert.equal('talkgroup' in talkgroup, false);
});

test('a failed save keeps the dirty working copy intact', async () => {
  const { store, useCases } = setup({ token: 'jwt-0', apiOptions: { saveError: true } });

  await useCases.boot();

  useCases.setSectionWorking('groups', [{ id: 1, label: 'Renamed' }]);

  const ok = await useCases.saveSection('groups');

  assert.equal(ok, false);

  const slice = store.getState()['section:groups'];

  assert.equal(slice.dirty, true);
  assert.deepEqual(slice.working, [{ id: 1, label: 'Renamed' }]);
});

test('an invalid section cannot be saved', async () => {
  const { api, store, useCases } = setup({ token: 'jwt-0' });

  await useCases.boot();

  useCases.setSectionWorking('groups', [{ id: 1, label: '' }]);
  useCases.setSectionInvalid('groups', true);

  const ok = await useCases.saveSection('groups');

  assert.equal(ok, false);
  assert.equal(api.calls.some((c) => c[0] === 'saveSection'), false);
});

test('discardSection reverts the working copy to the baseline', async () => {
  const { store, useCases } = setup({ token: 'jwt-0' });

  await useCases.boot();

  useCases.setSectionWorking('groups', [{ id: 1, label: 'Renamed' }]);

  useCases.discardSection('groups');

  const slice = store.getState()['section:groups'];

  assert.deepEqual(slice.working, [{ id: 1, label: 'Dispatch' }]);
  assert.equal(slice.dirty, false);
  assert.equal(slice.conflict, false);
});

test('reloadSection fetches the section and clears the conflict', async () => {
  const { sockets, store, useCases } = setup({
    token: 'jwt-0',
    apiOptions: { fetchedSections: { groups: [{ id: 1, label: 'Fresh' }] } },
  });

  await useCases.boot();

  useCases.setSectionWorking('groups', [{ id: 1, label: 'Mine' }]);

  sockets.opened[0].emit({ type: 'config', config: { ...sectionConfig(), groups: [{ id: 1, label: 'Theirs' }] } });

  const ok = await useCases.reloadSection('groups');

  assert.equal(ok, true);

  const slice = store.getState()['section:groups'];

  assert.equal(slice.conflict, false);
  assert.equal(slice.dirty, false);
  assert.deepEqual(slice.baseline, [{ id: 1, label: 'Fresh' }]);
  assert.deepEqual(slice.working, [{ id: 1, label: 'Fresh' }]);
});

test('changePassword updates the passwordNeedChange auth flag on success', async () => {
  const { api, store, useCases } = setup({ token: 'jwt-0' });

  const ok = await useCases.changePassword('old', 'new-secret');

  assert.equal(ok, true);
  assert.deepEqual(api.calls.at(-1), ['changePassword', 'old', 'new-secret']);
  assert.equal(store.getState().auth.passwordNeedChange, false);
});

test('changePassword reports failure without touching the auth flags', async () => {
  const { store, useCases } = setup({ token: 'jwt-0', apiOptions: { changePasswordError: true } });

  await useCases.boot();

  assert.equal(store.getState().auth.passwordNeedChange, true);

  const ok = await useCases.changePassword('wrong', 'new-secret');

  assert.equal(ok, false);
  assert.equal(store.getState().auth.passwordNeedChange, true);
});

// --- the control a todo names -------------------------------------------------

test('a todo that names the control clearing it raises the request the section answers', () => {
  const { store, useCases } = setup();

  useCases.focusControl('options', 'autoPopulate');

  assert.deepEqual(store.getState().focus, { section: 'options', control: 'autoPopulate' });
});

test('the section that answered the request leaves nothing standing for the next visit', () => {
  const { store, useCases } = setup();

  useCases.focusControl('options', 'autoPopulate');

  useCases.clearFocus();

  assert.equal(store.getState().focus, null);
});
