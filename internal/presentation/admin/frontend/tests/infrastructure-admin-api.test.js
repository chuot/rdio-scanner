import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { createAdminApi } from '../infrastructure/admin-api.js';
import { createTokenStorage } from '../infrastructure/token-storage.js';

function createFakes() {
    const requests = [];
    const responses = new Map();

    const sessionStore = new Map();
    const sessionStorage = {
        getItem: (key) => (sessionStore.has(key) ? sessionStore.get(key) : null),
        setItem: (key, value) => sessionStore.set(key, String(value)),
        removeItem: (key) => sessionStore.delete(key),
    };

    const fetchImpl = async (url, options = {}) => {
        requests.push({ url, options });

        const canned = responses.get(url) ?? { status: 200, body: {} };

        return {
            ok: canned.status >= 200 && canned.status < 300,
            status: canned.status,
            json: async () => canned.body,
        };
    };

    const respond = (url, status, body) => responses.set(url, { status, body });

    return { requests, respond, storage: createTokenStorage({ sessionStorage }), fetchImpl };
}

describe('infrastructure/admin-api — login and token', () => {
    it('posts the password and stores the returned token raw in the session', async () => {
        const fakes = createFakes();
        fakes.respond('/api/admin/login', 200, { token: 'jwt-1', passwordNeedChange: true });

        const api = createAdminApi(fakes);

        const result = await api.login('password');

        assert.deepEqual(result, { token: 'jwt-1', passwordNeedChange: true });
        assert.equal(fakes.storage.getAdminToken(), 'jwt-1');

        const [request] = fakes.requests;
        assert.equal(request.url, '/api/admin/login');
        assert.equal(request.options.method, 'POST');
        assert.deepEqual(JSON.parse(request.options.body), { password: 'password' });
    });

    it('sends the raw token in Authorization without a Bearer prefix', async () => {
        const fakes = createFakes();
        fakes.storage.setAdminToken('jwt-1');

        const api = createAdminApi(fakes);

        await api.getConfig();

        assert.deepEqual(fakes.requests[0].options.headers, { Authorization: 'jwt-1' });
    });

    it('logout posts and clears the token', async () => {
        const fakes = createFakes();
        fakes.storage.setAdminToken('jwt-1');

        const api = createAdminApi(fakes);

        await api.logout();

        assert.equal(fakes.requests[0].url, '/api/admin/logout');
        assert.equal(fakes.requests[0].options.method, 'POST');
        assert.equal(fakes.storage.getAdminToken(), '');
    });
});

describe('infrastructure/admin-api — endpoints', () => {
    it('GET /api/admin/config returns the full payload', async () => {
        const fakes = createFakes();
        fakes.respond('/api/admin/config', 200, { config: { systems: [] }, docker: true, passwordNeedChange: false });

        const api = createAdminApi(fakes);

        const res = await api.getConfig();

        assert.equal(fakes.requests[0].options.method, 'GET');
        assert.equal(res.docker, true);
        assert.deepEqual(res.config, { systems: [] });
    });

    it('PUT /api/admin/config sends the full config body', async () => {
        const fakes = createFakes();
        fakes.respond('/api/admin/config', 200, { config: { systems: [1] } });

        const api = createAdminApi(fakes);

        const res = await api.saveConfig({ systems: [1] });

        assert.equal(fakes.requests[0].options.method, 'PUT');
        assert.deepEqual(JSON.parse(fakes.requests[0].options.body), { systems: [1] });
        assert.deepEqual(res.config, { systems: [1] });
    });

    it('GET /api/admin/alerts', async () => {
        const fakes = createFakes();
        fakes.respond('/api/admin/alerts', 200, { A: [{ begin: 0, end: 1, frequency: 500, type: 'sine' }] });

        const api = createAdminApi(fakes);

        const alerts = await api.getAlerts();

        assert.ok(alerts.A);
    });

    it('POST /api/admin/logs with search options', async () => {
        const fakes = createFakes();
        fakes.respond('/api/admin/logs', 200, { count: 0, logs: [] });

        const api = createAdminApi(fakes);

        await api.searchLogs({ limit: 200, offset: 0, sort: -1, level: 'warn' });

        assert.equal(fakes.requests[0].url, '/api/admin/logs');
        assert.deepEqual(JSON.parse(fakes.requests[0].options.body), { limit: 200, offset: 0, sort: -1, level: 'warn' });
    });

    it('POST /api/admin/password', async () => {
        const fakes = createFakes();
        fakes.respond('/api/admin/password', 200, { passwordNeedChange: false });

        const api = createAdminApi(fakes);

        const res = await api.changePassword('old', 'new');

        assert.deepEqual(JSON.parse(fakes.requests[0].options.body), { currentPassword: 'old', newPassword: 'new' });
        assert.equal(res.passwordNeedChange, false);
    });
});

describe('infrastructure/admin-api — per-section endpoints', () => {
    it('GET /api/admin/config/<section> returns just that section', async () => {
        const fakes = createFakes();
        fakes.respond('/api/admin/config/groups', 200, [{ id: 1, label: 'Dispatch' }]);

        const api = createAdminApi(fakes);

        const groups = await api.getSection('groups');

        assert.equal(fakes.requests[0].url, '/api/admin/config/groups');
        assert.equal(fakes.requests[0].options.method, 'GET');
        assert.deepEqual(groups, [{ id: 1, label: 'Dispatch' }]);
    });

    it('PUT /api/admin/config/<section> sends the section value and returns the saved section', async () => {
        const fakes = createFakes();
        fakes.respond('/api/admin/config/tags', 200, [{ id: 2, label: 'Law' }]);

        const api = createAdminApi(fakes);

        const tags = await api.saveSection('tags', [{ label: 'Law' }]);

        assert.equal(fakes.requests[0].url, '/api/admin/config/tags');
        assert.equal(fakes.requests[0].options.method, 'PUT');
        assert.deepEqual(JSON.parse(fakes.requests[0].options.body), [{ label: 'Law' }]);
        assert.deepEqual(tags, [{ id: 2, label: 'Law' }]);
    });

    it('rejects unknown section names before any request', async () => {
        const fakes = createFakes();

        const api = createAdminApi(fakes);

        await assert.rejects(() => api.getSection('bogus'));
        await assert.rejects(() => api.saveSection('bogus', []));
        assert.equal(fakes.requests.length, 0);
    });

    it('a 417 persistence failure rejects', async () => {
        const fakes = createFakes();
        fakes.respond('/api/admin/config/groups', 417, {});

        const api = createAdminApi(fakes);

        await assert.rejects(() => api.saveSection('groups', []));
    });
});

describe('infrastructure/admin-api — 401 session end', () => {
    it('a 401 clears the token and notifies the session handler', async () => {
        const fakes = createFakes();
        fakes.storage.setAdminToken('jwt-stale');
        fakes.respond('/api/admin/config', 401, {});

        let unauthorized = 0;
        const api = createAdminApi({ ...fakes, onUnauthorized: () => unauthorized++ });

        await assert.rejects(() => api.getConfig());

        assert.equal(fakes.storage.getAdminToken(), '');
        assert.equal(unauthorized, 1);
    });
});

describe('infrastructure/admin-api — the live status read', () => {
    it('reads the status with the same raw credential the rest of the surface uses', async () => {
        const fakes = createFakes();
        fakes.storage.setAdminToken('jwt-1');
        fakes.respond('/api/admin/status', 200, { server: { uptimeSeconds: 12 }, live: {}, activity: {}, logs: {} });

        const api = createAdminApi(fakes);

        const status = await api.getStatus();

        assert.deepEqual(status, { server: { uptimeSeconds: 12 }, live: {}, activity: {}, logs: {} });

        const [request] = fakes.requests;
        assert.equal(request.url, '/api/admin/status');
        assert.equal(request.options.method, 'GET');
        assert.equal(request.options.body, undefined, 'the status read sends a body');
        assert.deepEqual(request.options.headers, { Authorization: 'jwt-1' });
    });

    it('a 417 status read rejects and keeps the session', async () => {
        const fakes = createFakes();
        fakes.storage.setAdminToken('jwt-1');
        fakes.respond('/api/admin/status', 417, undefined);

        let unauthorized = 0;
        const api = createAdminApi({ ...fakes, onUnauthorized: () => unauthorized++ });

        await assert.rejects(() => api.getStatus());

        assert.equal(fakes.storage.getAdminToken(), 'jwt-1', 'a read the server could not complete ended the session');
        assert.equal(unauthorized, 0, 'a read the server could not complete was treated as an expired session');
    });

    it('a 401 status read ends the session the way every other administration read does', async () => {
        const fakes = createFakes();
        fakes.storage.setAdminToken('jwt-stale');
        fakes.respond('/api/admin/status', 401, {});

        let unauthorized = 0;
        const api = createAdminApi({ ...fakes, onUnauthorized: () => unauthorized++ });

        await assert.rejects(() => api.getStatus());

        assert.equal(fakes.storage.getAdminToken(), '');
        assert.equal(unauthorized, 1);
    });
});
