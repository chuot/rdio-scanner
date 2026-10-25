// admin REST adapter. Auth: raw JWT in Authorization, no Bearer. 401 clears it.
// token and ends the session.

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

    // the talkgroups level saves beside the system's blacklist: a drop and storing
    // the entry that refuses it share one request; a save naming no blacklist sends
    // sends the list it has always sent.
    async saveSubSection(systemId, level, value, blacklists) {
      assertSubLevel(level);

      const body = typeof blacklists === 'string' ? { [level]: value, blacklists } : value;

      return request('PUT', `/config/systems/${systemId}/${level}`, body);
    },

    getAlerts() {
      return request('GET', '/alerts');
    },

    // one read answers the whole dashboard snapshot; the server's 417 arrives as a
    // rejection, so the page says the read failed instead of showing an idle server.
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
