// admin use-cases: login/logout, session lifecycle, config socket, per-section.
// each section is a `section:<name>` slice: baseline, working, dirty, invalid.

import { computeTodos } from '../domain/todos.js';
import { normalizeDirwatchRow } from '../domain/dirwatch.js';
import { blacklistsTextInvalid } from '../domain/systems-validation.js';
import { appendBlacklistsEntry } from '../domain/blacklists.js';
import {
  ADMIN_CONFIG_SECTIONS,
  SYSTEM_SUB_LEVELS,
  createSectionWorkingCopy,
  createSubSectionWorkingCopy,
  serializeSection,
  serializeSubSection,
} from '../../../common/frontend/domain/config.js';
import { deepEqual } from '../../../common/frontend/application/store.js';

const sliceKey = (name) => `section:${name}`;

const subSliceKey = (level, systemId) => `subsection:${level}:${systemId}`;

// the blacklist edited by a talkgroups section travels as its own keyed slice.
const blacklistsSliceKey = (systemId) => `blacklists:${systemId}`;

// the stored dirwatch delay sits below the section minimum; the loader applies the
// section's own clamp, so a saved row reads valid and unchanged. Others load.
function loadSectionRows(name, rows) {
  return name === 'dirwatch' && rows ? rows.map(normalizeDirwatchRow) : rows;
}

function freshSectionState(name, baseline) {
  return {
    baseline,
    working: createSectionWorkingCopy(name, baseline),
    dirty: false,
    invalid: false,
    conflict: false,
  };
}

function freshSubState(level, systemId, baseline) {
  return {
    baseline,
    working: createSubSectionWorkingCopy(level, baseline),
    dirty: false,
    invalid: false,
    conflict: false,
  };
}

function freshBlacklistsState(systemId, baseline) {
  const text = baseline ?? '';

  return {
    systemId,
    baseline: text,
    working: text,
    dirty: false,
    invalid: blacklistsTextInvalid(text),
    conflict: false,
  };
}

export function createAdminUseCases({ api, storage, createSocket, store }) {
  let socket = null;

  function setAuth(patch) {
    store.setState({ auth: { ...store.getState().auth, ...patch } });
  }

  // asks the owning section to show the control a todo names; one-shot request.
  function focusControl(section, control) {
    store.setState({ focus: { section, control } });
  }

  function clearFocus() {
    store.setState({ focus: null });
  }

  function openSocket() {
    if (socket) return;

    const token = storage.getAdminToken();
    if (!token) return;

    socket = createSocket(token);

    socket.subscribe((event) => {
      if (event.type === 'config') {
        applyConfig(event.config);

        // a save changes what the server runs; the dashboard shows that, so it
        // reloads.
        loadStatus();
      }

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
      const baseline = loadSectionRows(name, config?.[name] ?? null);

      if (current && (current.dirty || current.conflict)) {
        // Local edits are preserved; the section is flagged as a conflict
        // unless the pushed value happens to match the working copy.
        const dirty = !deepEqual(current.working, baseline);

        patch[key] = { ...current, baseline, dirty, conflict: dirty };
      } else {
        patch[key] = freshSectionState(name, baseline);
      }
    }

    // every system level gets its own slice, plus one for its talkgroups blacklist.
    for (const system of config?.systems ?? []) {
      for (const level of SYSTEM_SUB_LEVELS) {
        const key = subSliceKey(level, system.id);
        const current = store.getState()[key];
        const baseline = system[level] ?? null;

        if (current && (current.dirty || current.conflict)) {
          const dirty = !deepEqual(current.working, baseline);

          patch[key] = { ...current, baseline, dirty, conflict: dirty };
        } else {
          patch[key] = freshSubState(level, system.id, baseline);
        }
      }

      patch[blacklistsSliceKey(system.id)] = blacklistsState(system, store.getState()[blacklistsSliceKey(system.id)]);
    }

    store.setState(patch);
  }

  // a systems save re-seeds blacklist slices: untouched slices follow stored text,
  // an edited slice conflicts.
  function blacklistsState(system, current) {
    const baseline = system.blacklists ?? '';

    if (current && (current.dirty || current.conflict)) {
      const dirty = current.working !== baseline;

      return { ...current, baseline, dirty, conflict: dirty };
    }

    return freshBlacklistsState(system.id, baseline);
  }

  function updateSectionAndConfig(name, baseline) {
    const loaded = loadSectionRows(name, baseline);
    const patch = { [sliceKey(name)]: freshSectionState(name, loaded) };

    if (name === 'systems') {
      for (const system of loaded ?? []) {
        const key = blacklistsSliceKey(system.id);

        patch[key] = blacklistsState(system, store.getState()[key]);
      }
    }

    const config = store.getState().config;

    if (config) patch.config = { ...config, [name]: loaded };

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

  // the dashboard's live status snapshot; one read at a
  // time.
  let statusRead = null;

  async function loadStatus() {
    if (statusRead) return statusRead;

    statusRead = (async () => {
      const held = store.getState().status ?? { snapshot: null, failed: false, fetchedAt: null };

      try {
        const snapshot = await api.getStatus();

        // fetchedAt is the instant the server says it read: the only honest stamp.
        const status = { snapshot, failed: false, fetchedAt: snapshot?.server?.now ?? null };

        store.setState({ status });

        return status;
      } catch {
        // a failed read keeps the held numbers, marked failed: dropping them loses them
        // actionable data, and unmarked they would read as current.
        const status = { ...held, failed: true };

        store.setState({ status });

        return status;
      } finally {
        statusRead = null;
      }
    })();

    return statusRead;
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
    if (!password) return 'invalid';

    try {
      const res = await api.login(password);

      setAuth({ authenticated: true, passwordNeedChange: !!res.passwordNeedChange });

      openSocket();

      await loadConfig();

      return 'ok';
    } catch (error) {
      // a TypeError never reached the server; Unauthorized is a wrong password or limit
      // the message must say which, or it is false.
      return error instanceof TypeError ? 'unreachable' : 'invalid';
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

  // --- per-section editing state ------------------------------------------

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

  function updateSubSectionAndConfig(level, systemId, baseline, blacklists) {
    const key = subSliceKey(level, systemId);

    const patch = { [key]: freshSubState(level, systemId, baseline) };

    if (blacklists !== undefined) {
      const current = store.getState()[blacklistsSliceKey(systemId)];

      patch[blacklistsSliceKey(systemId)] = {
        ...current,
        baseline: blacklists,
        working: blacklists,
        dirty: false,
        invalid: blacklistsTextInvalid(blacklists),
        conflict: false,
      };
    }

    const config = store.getState().config;

    if (config) {
      patch.config = {
        ...config,
        systems: (config.systems ?? []).map((system) => (system.id === systemId ? { ...system, [level]: baseline } : system)),
      };

      if (blacklists !== undefined) {
        patch.config.systems = patch.config.systems.map((system) => (system.id === systemId ? { ...system, blacklists } : system));
      }
    }

    // a level save that changes a blacklist must leave Systems holding the server list
    // server's new list, baseline and working copy, or its next save overwrites it.
    const systems = store.getState()[sliceKey('systems')];

    if (systems && blacklists !== undefined) {
      const carried = (rows) => (rows ?? []).map((system) => (system.id === systemId ? { ...system, blacklists } : system));

      const baseline = carried(systems.baseline);
      const working = carried(systems.working);

      patch[sliceKey('systems')] = {
        ...systems,
        baseline,
        working,
        dirty: !deepEqual(working, baseline),
      };
    }

    store.setState(patch);
  }

  function setSubSectionWorking(level, systemId, value) {
    const current = store.getState()[subSliceKey(level, systemId)];
    if (!current) return;

    const dirty = !deepEqual(value, current.baseline);

    store.setState({ [subSliceKey(level, systemId)]: { ...current, working: value, dirty } });
  }

  function setSubSectionInvalid(level, systemId, invalid) {
    const current = store.getState()[subSliceKey(level, systemId)];
    if (!current) return;

    store.setState({ [subSliceKey(level, systemId)]: { ...current, invalid } });
  }

  async function saveSubSection(level, systemId) {
    const current = store.getState()[subSliceKey(level, systemId)];

    if (!current || current.invalid) return false;

    // dropping a talkgroup and storing the entry that refuses it: one request.
    const blacklists = level === 'talkgroups' ? store.getState()[blacklistsSliceKey(systemId)] : null;

    if (blacklists?.invalid) return false;

    try {
      const saved = await api.saveSubSection(systemId, level, serializeSubSection(level, current.working), blacklists?.working);

      updateSubSectionAndConfig(level, systemId, saved, blacklists?.working);

      return true;
    } catch {
      // 401 already ended the session; 400/404/417 keep the dirty working copy.
      return false;
    }
  }

  async function reloadSubSection(level, systemId) {
    try {
      const value = await api.getSubSection(systemId, level);

      // reload starts from the last save's text, not the working copy.
      updateSubSectionAndConfig(level, systemId, value, store.getState()[blacklistsSliceKey(systemId)]?.baseline);

      return true;
    } catch {
      return false;
    }
  }

  function discardSubSection(level, systemId) {
    const current = store.getState()[subSliceKey(level, systemId)];
    if (!current) return;

    const patch = {
      [subSliceKey(level, systemId)]: {
        ...current,
        working: createSubSectionWorkingCopy(level, current.baseline),
        dirty: false,
        invalid: false,
        conflict: false,
      },
    };

    // the added entry is part of the same unsaved change: Discard removes it too.
    if (level === 'talkgroups') {
      const key = blacklistsSliceKey(systemId);
      const blacklists = store.getState()[key];

      if (blacklists) {
        patch[key] = {
          ...blacklists,
          working: blacklists.baseline,
          dirty: false,
          invalid: blacklistsTextInvalid(blacklists.baseline),
          conflict: false,
        };
      }
    }

    store.setState(patch);
  }

  function setBlacklistsWorking(systemId, value) {
    const current = store.getState()[blacklistsSliceKey(systemId)];
    if (!current) return;

    const text = value ?? '';

    store.setState({
      [blacklistsSliceKey(systemId)]: {
        ...current,
        working: text,
        dirty: text !== current.baseline,
        invalid: blacklistsTextInvalid(text),
      },
    });
  }

  // the entry joins the blacklist, the row leaves talkgroups: one save stores both.
  function blacklistTalkgroup(systemId, talkgroupRef) {
    const current = store.getState()[blacklistsSliceKey(systemId)];

    setBlacklistsWorking(systemId, appendBlacklistsEntry(current?.working, talkgroupRef));
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
    loadStatus,
    changePassword,
    setSectionWorking,
    setSectionInvalid,
    saveSection,
    reloadSection,
    discardSection,
    setSubSectionWorking,
    setSubSectionInvalid,
    setBlacklistsWorking,
    blacklistTalkgroup,
    saveSubSection,
    reloadSubSection,
    discardSubSection,
    focusControl,
    clearFocus,
  };
}
