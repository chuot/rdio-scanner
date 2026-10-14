// Admin use-cases: login/logout orchestration, session lifecycle, the admin
// config socket wiring, and per-section config state:
// each of the 8 sections lives in a `section:<name>` store slice holding
// { baseline, working, dirty, invalid, conflict }.

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

// The blacklist a system's talkgroups section edits beside its rows. It is stored on the system row,
// and it is saved with the talkgroups that were edited next to it, so it travels as its own slice
// keyed by the system rather than riding in the Systems section's working copy.
const blacklistsSliceKey = (systemId) => `blacklists:${systemId}`;

// The dirwatch rows the server answers carry the shape the server stores: a delay clamped to one
// day, below the floor the section's rule accepts. They load through the legacy form's own clamp,
// so a saved row reads valid and is not a change. Every other section loads as it is.
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

  // A todo that names the control clearing it asks the section that owns that control
  // to show it. The request is one-shot: the section consumes it, so the mark does not
  // outlive the visit that asked for it.
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

    // Every level of every system gets its own slice, with the same
    // dirty/conflict preservation the sections use. The blacklist the talkgroups section edits
    // beside its rows gets one too, keyed by the system it belongs to: it is stored on the system
    // row and saved with the rows it was edited next to.
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

  // The systems section answers a save with the systems as the store holds them, blacklist text
  // included. The talkgroups section edits that text beside its rows, so a systems save re-seeds
  // each system's blacklist slice the way a server broadcast does: a slice the operator has not
  // changed follows the stored text, and a slice holding their own edit is flagged as a conflict.
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
      // A fetch that never reached the server is a TypeError; a wrong password or
      // a rate limit is the adapter's Unauthorized. The operator must be told
      // which one happened: "Invalid password" over a stopped server is a lie.
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

    // The system's blacklist is a field of the system row, and the Systems section saves every row it
    // holds, so the list that section is holding is the list the server ends up storing. A level save
    // that stored the entry therefore has to leave that section holding the list the server now holds,
    // in the saved copy and in the copy the operator is working on: a section that kept the text it was
    // showing would write that text back over the entry the operator just asked for, and the talkgroup
    // row the entry names is already gone. The section keeps asking to be saved for the edits the
    // operator made in the other fields, which is what the copy they are working on still carries.
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

    // The talkgroups level is edited beside the system's blacklist, and the save that drops a
    // talkgroup stores the entry that refuses it: the two halves of one action leave in one request.
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

      // The level answers its rows. The blacklist saved with them is the text the last save of this
      // level stored, so a reload starts from that text and drops an entry the operator has not saved.
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

    // The blacklist an entry was added to is part of the same unsaved change: Discard in the
    // talkgroups section takes the entry back out with the row it put back.
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

  // Blacklisting a talkgroup: the entry joins the system's blacklist and the row leaves the
  // talkgroups. Both are unsaved until the talkgroups section is saved, which stores them together.
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
