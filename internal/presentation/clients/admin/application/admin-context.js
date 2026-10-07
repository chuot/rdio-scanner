// Admin page context: composition root for the /admin entry only. Wires the
// admin REST adapter, the config WebSocket, the admin store, and the admin
// use-cases. Never touches the listener socket or the listener context.

import { createTokenStorage } from '../../admin/infrastructure/token-storage.js';
import { createAdminApi } from '../infrastructure/admin-api.js';
import { createAdminSocket } from '../infrastructure/admin-socket.js';
import { ADMIN_CONFIG_SECTIONS } from '../../shared/domain/config.js';
import { createStore } from '../../shared/application/store.js';
import { createAdminUseCases } from './admin-use-cases.js';

export function createAdminContext({ location, sessionStorage }) {
  const storage = createTokenStorage({ sessionStorage });

  const sections = Object.fromEntries(
    ADMIN_CONFIG_SECTIONS.map((name) => [
      `section:${name}`,
      { baseline: null, working: null, dirty: false, invalid: false, conflict: false },
    ]),
  );

  const store = createStore({
    auth: { authenticated: false, docker: false, passwordNeedChange: false },
    config: null,
    todos: [],
    alerts: {},
    ...sections,
  });

  const socketUrl = new URL('/api/admin/config', location.href).href.replace(/^http/, 'ws');

  let useCases;

  const api = createAdminApi({
    storage,
    onUnauthorized: () => useCases?.sessionExpired(),
  });

  useCases = createAdminUseCases({
    api,
    storage,
    store,
    createSocket: (token) => createAdminSocket({ url: socketUrl, token }),
  });

  return { api, storage, store, useCases };
}

let context = null;

export function getAdminContext() {
  if (!context) {
    context = createAdminContext({
      location: window.location,
      sessionStorage: window.sessionStorage,
    });
  }

  return context;
}
