// Admin use-cases: login/logout orchestration, session lifecycle, the admin
// config socket wiring, and per-section config state (T035, REQ-019):
// each of the 8 sections lives in a `section:<name>` store slice holding
// { baseline, working, dirty, invalid, conflict }.

import { computeTodos } from '../domain/todos.js';
import { ADMIN_CONFIG_SECTIONS, createSectionWorkingCopy, serializeSection } from '../../../common/frontend/domain/config.js';
import { deepEqual } from '../../../common/frontend/application/store.js';

const sliceKey = (name) => `section:${name}`;

function freshSectionState(name, baseline) {
  return {
    baseline,
    working: createSectionWorkingCopy(name, baseline),
    dirty: false,
    invalid: false,
    conflict: false,
  };
}

export function createAdminUseCases({ api, storage, createSocket, store }) {
  let socket = null;

  function setAuth(patch) {
    store.setState({ auth: { ...store.getState().auth, ...patch } });
  }

  function openSocket() {
    if (socket) return;

    const token = storage.getAdminToken();
    if (!token) return;

    socket = createSocket(token);

    socket.subscribe((event) => {
      if (event.type === 'config') applyConfig(event.config);
      if (event.type === 'unauthorized') sessionExpired();
    });

    socket.connect();
  }

  function closeSocket() {
    if (!socket) return;

    socket.close();
    socket = null;
  }

  function applyConfig(config) {
    const todos = computeTodos(config, {
      passwordNeedChange: store.getState().auth.passwordNeedChange,
    });

    const patch = { config, todos };

    for (const name of ADMIN_CONFIG_SECTIONS) {
      const key = sliceKey(name);
      const current = store.getState()[key];
      const baseline = config?.[name] ?? null;

      if (current && (current.dirty || current.conflict)) {
        // Local edits are preserved; the section is flagged as a conflict
        // unless the pushed value happens to match the working copy.
        const dirty = !deepEqual(current.working, baseline);

        patch[key] = { ...current, baseline, dirty, conflict: dirty };
      } else {
        patch[key] = freshSectionState(name, baseline);
      }
    }

    store.setState(patch);
  }

  function updateSectionAndConfig(name, baseline) {
    const patch = { [sliceKey(name)]: freshSectionState(name, baseline) };

    const config = store.getState().config;

    if (config) patch.config = { ...config, [name]: baseline };

    store.setState(patch);
  }

  async function loadConfig() {
    try {
      const res = await api.getConfig();

      setAuth({ docker: !!res.docker, passwordNeedChange: !!res.passwordNeedChange });

      applyConfig(res.config);

      let alerts = {};

      try {
        alerts = (await api.getAlerts()) ?? {};
      } catch {
        // alert audition degrades to no sequences
      }

      store.setState({ alerts });

      return res.config;
    } catch {
      // 401 handling already ended the session through the api adapter;
      // other failures leave the panel usable with an empty config.
      return null;
    }
  }

  function sessionExpired() {
    storage.clearAdminToken();

    closeSocket();

    setAuth({ authenticated: false });
  }

  async function boot() {
    if (!storage.getAdminToken()) {
      setAuth({ authenticated: false });

      return false;
    }

    setAuth({ authenticated: true });

    openSocket();

    const config = await loadConfig();

    if (config === null) sessionExpired();

    return store.getState().auth.authenticated;
  }

  async function login(password) {
    if (!password) return false;

    try {
      const res = await api.login(password);

      setAuth({ authenticated: true, passwordNeedChange: !!res.passwordNeedChange });

      openSocket();

      await loadConfig();

      return true;
    } catch {
      return false;
    }
  }

  async function logout() {
    try {
      await api.logout();
    } catch {
      // the token is being dropped anyway
    }

    sessionExpired();

    return true;
  }

  // --- per-section editing state (T035) ------------------------------------

  function setSectionWorking(name, value) {
    const current = store.getState()[sliceKey(name)];
    if (!current) return;

    const dirty = !deepEqual(value, current.baseline);

    store.setState({ [sliceKey(name)]: { ...current, working: value, dirty } });
  }

  function setSectionInvalid(name, invalid) {
    const current = store.getState()[sliceKey(name)];
    if (!current) return;

    store.setState({ [sliceKey(name)]: { ...current, invalid } });
  }

  async function saveSection(name) {
    const current = store.getState()[sliceKey(name)];

    if (!current || current.invalid) return false;

    try {
      const saved = await api.saveSection(name, serializeSection(name, current.working));

      updateSectionAndConfig(name, saved);

      return true;
    } catch {
      // 401 already ended the session; 400/417 keep the dirty working copy.
      return false;
    }
  }

  async function reloadSection(name) {
    try {
      const value = await api.getSection(name);

      updateSectionAndConfig(name, value);

      return true;
    } catch {
      return false;
    }
  }

  function discardSection(name) {
    const current = store.getState()[sliceKey(name)];
    if (!current) return;

    store.setState({
      [sliceKey(name)]: {
        ...current,
        working: createSectionWorkingCopy(name, current.baseline),
        dirty: false,
        invalid: false,
        conflict: false,
      },
    });
  }

  async function changePassword(currentPassword, newPassword) {
    try {
      const res = await api.changePassword(currentPassword, newPassword);

      setAuth({ passwordNeedChange: !!res?.passwordNeedChange });

      return true;
    } catch {
      return false;
    }
  }

  return {
    boot,
    login,
    logout,
    sessionExpired,
    loadConfig,
    changePassword,
    setSectionWorking,
    setSectionInvalid,
    saveSection,
    reloadSection,
    discardSection,
  };
}
