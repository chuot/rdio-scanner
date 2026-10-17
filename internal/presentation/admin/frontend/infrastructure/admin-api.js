// Admin REST adapter (legacy admin.service.ts HttpClient calls).
// Auth: raw JWT in Authorization, no Bearer prefix. 401 → clear token, end session.

import { ADMIN_CONFIG_SECTIONS, SYSTEM_SUB_LEVELS } from '../../../common/frontend/domain/config.js';

export function createAdminApi({ fetchImpl = globalThis.fetch, storage, onUnauthorized = () => {} }) {
  async function request(method, path, body) {
    const res = await fetchImpl(`/api/admin${path}`, {
      method,
      headers: { Authorization: storage.getAdminToken() },
      ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
    });

    if (res.status === 401) {
      storage.clearAdminToken();
      onUnauthorized();

      throw new Error('Unauthorized');
    }

    if (!res.ok) {
      throw new Error(`Admin API ${method} ${path}: ${res.status}`);
    }

    // POST /logout answers 200 with an empty body.
    try {
      return await res.json();
    } catch {
      return null;
    }
  }

  return {
    async login(password) {
      const res = await request('POST', '/login', { password });

      storage.setAdminToken(res.token);

      return { token: res.token, passwordNeedChange: res.passwordNeedChange };
    },

    async logout() {
      await request('POST', '/logout', null);

      storage.clearAdminToken();
    },

    getConfig() {
      return request('GET', '/config');
    },

    saveConfig(config) {
      return request('PUT', '/config', config);
    },

    // Per-section endpoints: GET/PUT /api/admin/config/<section>.
    async getSection(section) {
      assertSection(section);

      return request('GET', `/config/${section}`);
    },

    async saveSection(section, value) {
      assertSection(section);

      return request('PUT', `/config/${section}`, value);
    },

    // Per-level endpoints: GET/PUT /api/admin/config/systems/<id>/<level>.
    async getSubSection(systemId, level) {
      assertSubLevel(level);

      return request('GET', `/config/systems/${systemId}/${level}`);
    },

    // The talkgroups level of a system is saved as the list of its rows. It is also saved as the
    // object that carries those rows beside the system's blacklist, so the save that drops a
    // talkgroup stores the entry that refuses it in the same request. A level save that names no
    // blacklist sends the list it has always sent.
    async saveSubSection(systemId, level, value, blacklists) {
      assertSubLevel(level);

      const body = typeof blacklists === 'string' ? { [level]: value, blacklists } : value;

      return request('PUT', `/config/systems/${systemId}/${level}`, body);
    },

    getAlerts() {
      return request('GET', '/alerts');
    },

    // The live status of the server: one read that answers the whole snapshot the dashboard is built from.
    // A 417 — the server's own answer to a read it could not complete — arrives here as a rejection, which
    // is what lets the page say the read failed instead of showing a server that looks idle and healthy.
    getStatus() {
      return request('GET', '/status');
    },

    searchLogs(options) {
      return request('POST', '/logs', options);
    },

    changePassword(currentPassword, newPassword) {
      return request('POST', '/password', { currentPassword, newPassword });
    },
  };

  function assertSection(section) {
    if (!ADMIN_CONFIG_SECTIONS.includes(section)) {
      throw new Error(`Unknown admin config section: ${section}`);
    }
  }

  function assertSubLevel(level) {
    if (!SYSTEM_SUB_LEVELS.includes(level)) {
      throw new Error(`Unknown system sub-section: ${level}`);
    }
  }
}
