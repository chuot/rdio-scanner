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

function fakeApi({ loginResult = { token: 'jwt-1', passwordNeedChange: true }, config = { config: sectionConfig(), passwordNeedChange: true }, fetchedSections = {}, saveError = false, changePasswordError = false, dirwatchDelayClamp = false, status = null, storage } = {}) {
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

      // The real server clamps every stored dirwatch delay to one day: 1440. The fake answers the
      // way the server does, so the reading sees the shape the round-trip really carries.
      if (dirwatchDelayClamp && section === 'dirwatch') {
        return value.map((row) => ({ ...row, delay: Math.min(row.delay, 1440) }));
      }

      return structuredClone(value);
    },
    async changePassword(currentPassword, newPassword) {
      calls.push(['changePassword', currentPassword, newPassword]);

      if (changePasswordError) throw new Error('Admin API POST /password: 417');

      return { passwordNeedChange: false };
    },
    async getStatus() {
      calls.push(['getStatus']);

      if (status instanceof Error) throw status;

      return structuredClone(status);
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

// The systems section saves the fields of the system row. The sites, talkgroups, and units under it
// are saved by the section that shows them, for the system it shows: the rows this section loaded
// are older than a level the operator saved there, so sending them would store back rows that
// section deleted, and refuse talkgroups the system's blacklist has just taken.
test('saveSection sends the system row and leaves its levels to the sections that save them', async () => {
  const { api, useCases } = setup({ token: 'jwt-0' });

  await useCases.boot();

  useCases.setSectionWorking('systems', [{
    id: 1,
    label: 'S',
    sites: [{ id: 3, label: 'Site', order: 1, siteRef: 30 }],
    systemRef: 1,
    talkgroups: [{ id: 10, label: 'T', order: 7, talkgroupRef: 1514 }],
    units: [{ id: 8, label: 'Unit', order: 1, unitRef: 80 }],
  }]);

  await useCases.saveSection('systems');

  const call = api.calls.find((c) => c[0] === 'saveSection');
  const system = call[2][0];

  assert.equal(system.label, 'S', 'the fields of the system row are the systems section to save');
  assert.equal(system.systemRef, 1);

  for (const level of ['sites', 'talkgroups', 'units']) {
    assert.equal(system[level], undefined, `the systems save sends ${level}, which the ${level} section saves for the system it shows`);
  }
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

// The server clamps every stored dirwatch delay to one day: 1440, below the section's floor of 2000.
// The legacy form loaded the stored delay as Math.max(2000, delay) — the row read valid and the form
// was not dirty, because an initial control value is not a change. The application loads the server's
// rows through the same clamp, on the baseline and the working copy together, so the saved row means
// the same thing on both sides of its save.
test('a dirwatch row the server answers with its clamped delay loads as the floor the rule accepts, and is not a change', async () => {
  const { store, useCases } = setup({
    token: 'jwt-0',
    apiOptions: {
      config: {
        config: { ...sectionConfig(), dirwatch: [{ id: 1, order: 1, directory: '/var/spool/first', delay: 1440, deleteAfter: true, disabled: false, extension: 'mp3', frequency: 1, mask: '#SYS_#TG', type: 'default' }] },
        passwordNeedChange: true,
      },
    },
  });

  await useCases.boot();

  const slice = store.getState()['section:dirwatch'];

  assert.equal(slice.baseline[0].delay, 2000, 'the delay the server clamps to one day loads as the floor the rule accepts');
  assert.equal(slice.working[0].delay, 2000, 'the working copy carries the same delay, so the row is not a change');
  assert.equal(slice.dirty, false, 'a row loaded with the server clamped delay is not dirty');
});

test('saving a dirwatch row the server answers with its clamped delay leaves the section reading the floor, quiet, and not dirty', async () => {
  const { store, useCases } = setup({
    token: 'jwt-0',
    apiOptions: { dirwatchDelayClamp: true },
  });

  await useCases.boot();

  useCases.setSectionWorking('dirwatch', [{ directory: '/var/spool/first', delay: 2000, deleteAfter: true, disabled: false, frequency: 1, mask: '#SYS_#TG', order: 0, type: 'default' }]);

  const ok = await useCases.saveSection('dirwatch');

  assert.equal(ok, true);

  const slice = store.getState()['section:dirwatch'];

  assert.equal(slice.baseline[0].delay, 2000, 'the delay the server clamps to one day re-baselines as the floor the rule accepts');
  assert.equal(slice.working[0].delay, 2000, 'the working copy carries the same delay, so the saved row is not a change');
  assert.equal(slice.dirty, false, 'the section is quiet after its save');
});

// The live status snapshot the dashboard is built from (application/admin-use-cases.js's loadStatus). The
// page asks the server one question and holds the answer, and the two things the page has to get right are
// which answer it is holding and whether that answer is still current.
function statusSnapshot(overrides = {}) {
  return {
    server: {
      version: '8.0.0',
      docker: false,
      startedAt: '2026-10-17T06:31:03Z',
      now: '2026-10-17T08:45:11Z',
      uptimeSeconds: 8048,
      baseDir: '/data/rdio',
      databaseBytes: 4831838208,
      databaseKnown: true,
    },
    live: { listeners: 3, administratorSessions: 1, dirwatchersRunning: 2, heldCalls: 2, audioConversion: true, audioProbe: true },
    activity: { windowStart: '2026-10-16T08:00:00Z', windowEnd: '2026-10-17T08:00:00Z', callsToday: 214, callsLastHour: 37, hours: [], busiest: [], recent: [] },
    logs: { windowStart: '2026-10-16T08:45:11Z', errors: 3, warnings: 11, sources: [] },
    ...overrides,
  };
}

test('a successful status read writes the snapshot and the instant the server read it', async () => {
  const { api, store, useCases } = setup({ token: 'jwt-0', apiOptions: { status: statusSnapshot() } });

  await useCases.loadStatus();

  const status = store.getState().status;

  assert.equal(status.failed, false, 'a read that answered is held as a failure');
  assert.equal(status.snapshot.server.uptimeSeconds, 8048);
  assert.equal(status.snapshot.live.listeners, 3);

  // The instant the server says it read is the instant the answer is about, which is the only instant a
  // page can honestly attach to the figures it is showing.
  assert.equal(status.fetchedAt, '2026-10-17T08:45:11Z');

  assert.deepEqual(api.calls.map((c) => c[0]), ['getStatus']);
});

test('a status read the server refused sets the failed state and keeps the numbers the page holds', async () => {
  const { api, store, useCases } = setup({ token: 'jwt-0', apiOptions: { status: statusSnapshot() } });

  await useCases.loadStatus();

  const held = store.getState().status;

  assert.equal(held.failed, false);

  // The server goes away between two reads. Its answer to a status read it could not complete is 417 with
  // nothing in the body, which the adapter turns into a rejection.
  api.getStatus = async () => {
    api.calls.push(['getStatus']);

    throw new Error('Admin API GET /status: 417');
  };

  await useCases.loadStatus();

  const status = store.getState().status;

  assert.equal(status.failed, true, 'a read the server refused is not held as a read that answered');

  // The numbers are kept, and the failure is kept beside them: a page that drops the figures has thrown
  // away the only thing the operator can still act on, and a page that keeps them without the failure is
  // a page presenting them as current.
  assert.deepEqual(status.snapshot, held.snapshot, 'the page dropped the numbers it was holding');
  assert.equal(status.fetchedAt, held.fetchedAt, 'the page kept the numbers and lost the moment they were read');

  assert.equal(api.calls.filter((c) => c[0] === 'getStatus').length, 2);
});

test('a status read that never reached the server sets the failed state', async () => {
  const { store, useCases } = setup({ token: 'jwt-0', apiOptions: { status: new TypeError('fetch failed') } });

  await useCases.loadStatus();

  assert.equal(store.getState().status.failed, true, 'a read that never reached the server is not held as a success');
});

test('two status reads asked for in the same moment cost one request', async () => {
  const { api, useCases } = setup({ token: 'jwt-0', apiOptions: { status: statusSnapshot() } });

  const [first, second] = await Promise.all([useCases.loadStatus(), useCases.loadStatus()]);

  assert.equal(api.calls.length, 1, `the page asked the server ${api.calls.length} times for the same instant`);
  assert.equal(first, second);
});

test('a configuration push re-reads the status beside the configuration it applies', async () => {
  const { api, sockets, store, useCases } = setup({ token: 'jwt-0', apiOptions: { status: statusSnapshot() } });

  await useCases.boot();

  assert.deepEqual(api.calls.map((c) => c[0]), ['getConfig'], 'booting asked the server for a status read nobody has asked for yet');

  api.calls.length = 0;

  sockets.opened[0].emit({ type: 'config', config: { ...sectionConfig(), groups: [{ id: 1, label: 'Dispatch' }] } });

  assert.deepEqual(api.calls.map((c) => c[0]), ['getStatus'], 'a save the server broadcast did not ask for the answer the dashboard shows');

  await useCases.loadStatus();

  assert.equal(store.getState().config.groups.length, 1, 'the push did not apply the configuration it carried');
  assert.equal(store.getState().status.failed, false);
});
