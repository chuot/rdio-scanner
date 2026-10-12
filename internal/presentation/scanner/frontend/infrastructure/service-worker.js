/*
 * *****************************************************************************
 * Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>
 * ****************************************************************************
 */

/*
 * Hand-written service worker replacing the legacy Angular ngsw. The build
 * script injects the precache manifest below; a rebuild changes sw.js bytes,
 * which is what triggers the update flow.
 */

/* @precache-manifest */

const CACHE_NAME = 'rdio-scanner-static';

const UPDATE_CHECK_INTERVAL = 5 * 60 * 1000;

self.addEventListener('install', (event) => {
  // No skipWaiting: the new worker stays waiting until the user accepts
  // the update prompt (parity with the legacy AppUpdateService dialog).
  event.waitUntil(
    caches
      .open(CACHE_NAME)
      .then((cache) => cache.addAll(PRECACHE_MANIFEST))
      // An install that replaces a controlling worker is an update: tell
      // the open pages (ngsw's VERSION_READY equivalent). First installs
      // stay silent.
      .then(() => (self.registration.active ? notifyClients() : undefined)),
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      for (const key of await caches.keys()) {
        if (key !== CACHE_NAME) await caches.delete(key);
      }

      await self.clients.claim();
    })(),
  );
});

// The worker checks itself for an update every 5 minutes, like the legacy
// AppUpdateService polling SwUpdate.checkForUpdate().
setInterval(() => self.registration.update(), UPDATE_CHECK_INTERVAL);

self.addEventListener('message', (event) => {
  if (event.data && event.data.type === 'SKIP_WAITING') self.skipWaiting();
});

self.addEventListener('message', (event) => {
  if (event.data && event.data.type === 'SKIP_WAITING') self.skipWaiting();
});

self.addEventListener('fetch', (event) => {
  const request = event.request;

  if (request.method !== 'GET') return;

  const url = new URL(request.url);

  if (url.origin !== self.location.origin) return;

  // API calls and the WebSockets behind them are never intercepted.
  if (url.pathname.startsWith('/api/')) return;

  // The administration application is served from its own bundle and has no worker of its own. This
  // worker's scope is the whole origin, so without this it would answer an admin request out of the
  // listener's cache: the listener's shell for an admin route, and the listener's copy of a shared
  // font for an admin font.
  if (isAdminApplication(url)) return;

  if (request.mode === 'navigate') {
    event.respondWith(networkFirst(request, '/index.html'));
    return;
  }

  if (isHashedAsset(url)) {
    event.respondWith(cacheFirst(request));
    return;
  }

  event.respondWith(networkFirst(request));
});

// The administration application lives under /admin/. The server redirects /admin to that subtree, so
// both spellings are the administration application's paths, and neither is this worker's to answer.
function isAdminApplication(url) {
  return url.pathname === '/admin' || url.pathname.startsWith('/admin/');
}

// Content-hashed bundles and copied assets can never go stale by name. These are the names this build
// emits: one hashed module and one hashed stylesheet, and the asset directories copied beside them. The
// legacy client's admin., listener., styles. and chunk. bundles and its /media/ assets are emitted by
// nothing in this repository, and answering them from the cache would keep a file alive under a name the
// server now answers with its shell.
function isHashedAsset(url) {
  return /^\/main(\.[A-Za-z0-9]+)?\.(js|css)$/.test(url.pathname) || url.pathname.startsWith('/assets/');
}

async function cacheFirst(request) {
  const cache = await caches.open(CACHE_NAME);

  const cached = await cache.match(request);

  if (cached) return cached;

  const response = await fetch(request);

  if (response.ok) cache.put(request, response.clone());

  return response;
}

async function networkFirst(request, fallbackUrl) {
  const cache = await caches.open(CACHE_NAME);

  try {
    const response = await fetch(request);

    if (response.ok && request.mode !== 'navigate') cache.put(request, response.clone());

    return response;
  } catch {
    const cached = await cache.match(request);

    if (cached) return cached;

    if (fallbackUrl) {
      const shell = await cache.match(fallbackUrl);

      if (shell) return shell;
    }

    return new Response('Offline', { status: 504, headers: { 'Content-Type': 'text/plain' } });
  }
}

async function notifyClients() {
  for (const client of await self.clients.matchAll({ type: 'window', includeUncontrolled: true })) {
    client.postMessage({ type: 'AppUpdateAvailable' });
  }
}
