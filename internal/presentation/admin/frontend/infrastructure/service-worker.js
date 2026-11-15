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
 * Hand-written service worker for the administration application, written
 * beside its bundle under the name the built page registers, unhashed. The
 * build injects the precache manifest below; a rebuild changes this file's
 * own bytes, which is what triggers the update flow.
 *
 * Every path is resolved from where this worker sits, so the mount it is
 * served from is never named here.
 */

/* @precache-manifest */

const CACHE_NAME = 'rdio-admin-static';

// The listener application's cache is that application's worker's to manage;
// this activation leaves it on the origin.
const LISTENER_CACHE_NAME = 'rdio-scanner-static';

const UPDATE_CHECK_INTERVAL = 5 * 60 * 1000;

// The directory this worker sits in, which is the scope its registration asks
// for: the shell document, the emitted module and stylesheet, and the copied
// asset trees all sit under it.
const MOUNT = new URL('.', self.location).pathname;

// The document this application's shell is stored under.
const SHELL = new URL('index.html', self.location).href;

self.addEventListener('install', (event) => {
  // no skipWaiting: the new worker stays waiting until the operator accepts
  // the update prompt.
  event.waitUntil(
    caches
      .open(CACHE_NAME)
      .then((cache) => cache.addAll(PRECACHE_MANIFEST))
      // an install that replaces a controlling worker is an update: tell the
      // open pages. first installs stay silent.
      .then(() => (self.registration.active ? notifyClients() : undefined)),
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      for (const key of await caches.keys()) {
        if (key !== CACHE_NAME && key !== LISTENER_CACHE_NAME) await caches.delete(key);
      }

      await self.clients.claim();
    })(),
  );
});

// the worker checks itself for an update every 5 minutes.
setInterval(() => self.registration.update(), UPDATE_CHECK_INTERVAL);

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

  // paths outside this mount belong to the other application's worker.
  if (!isApplicationPath(url)) return;

  if (request.mode === 'navigate') {
    event.respondWith(networkFirst(request, SHELL));
    return;
  }

  if (isHashedAsset(url)) {
    event.respondWith(cacheFirst(request));
    return;
  }

  event.respondWith(networkFirst(request));
});

// this mount's own paths, read from where this worker sits: the registration
// scope already keeps every other path out of this worker's reach.
function isApplicationPath(url) {
  return url.pathname.startsWith(MOUNT);
}

// content-hashed bundles and copied assets can never go stale by name; these
// are the names this build emits. names an older build emitted, or a name the
// server answers for its own reasons, go to the network: caching them would
// keep a file alive under a name the server no longer answers.
function isHashedAsset(url) {
  const name = url.pathname.slice(MOUNT.length);

  return /^main(\.[A-Za-z0-9]+)?\.(js|css)$/.test(name) || name.startsWith('assets/');
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
    // a page this mount does not hold is the other application's page, and its
    // own worker asks it about its own new version.
    if (!isApplicationPath(new URL(client.url, self.location))) continue;

    client.postMessage({ type: 'AppUpdateAvailable' });
  }
}
