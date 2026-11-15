// tests for the administration service worker: what it stores, whom it tells, and how it answers
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import { describe, it } from 'node:test';

const here = dirname(fileURLToPath(import.meta.url));
const origin = 'https://rdio.example';

/** The worker sits beside the application's own infrastructure modules. */
const workerPath = join(here, '..', 'infrastructure', 'service-worker.js');

/** The listener application's worker, read beside it: the two name one cache each. */
const scannerWorkerPath = join(here, '..', '..', '..', 'scanner', 'frontend', 'infrastructure', 'service-worker.js');

/**
 * The names one administration build precaches, in the shape the built bundle has them: every one of them
 * under the mount the administration application is served from, which is also the scope its registration
 * asks for. The worker resolves its own paths from its location, so these are the paths it is asked about.
 */
const precached = [
    '/admin/index.html',
    '/admin/manifest.webmanifest',
    '/admin/sw.js',
    '/admin/favicon.ico',
    '/admin/main.AAA1111.js',
    '/admin/main.BBB2222.css',
    '/admin/assets/fonts/roboto-latin-w400.woff2',
    '/admin/assets/icons/icon-192x192.png',
];

/**
 * The worker is a script for a browser's worker global scope, not a module: it has no exports, and the
 * build injects its precache manifest into it. So the test reads its source, injects the manifest the way
 * the build does, and runs the result against a stand-in for that scope whose location is the worker's own
 * script URL. That location is what makes the mount rules readable: the worker resolves every path against
 * it, and the stand-in's clients and stored responses are named by path so a rule that reached the wrong
 * mount would answer differently.
 *
 * `calls` records the order the worker reached for the network and for the cache, which is the only visible
 * difference between the two strategies a fetch handler has.
 */
function loadWorker({ clients = [], cacheNames = [], stored = [], active = null, unreachable = [] } = {}) {
    const listeners = {};
    const calls = [];
    const messages = [];
    const deletions = [];
    const intervals = [];
    const storedNames = [];
    const claims = { count: 0 };
    const skips = { count: 0 };
    const added = [];

    const location = new URL(`${origin}/admin/sw.js`);

    const holds = new Map(
        stored.map((path) => [new URL(path, location).href, { ok: true, status: 200, clone: () => ({}), tag: path }]),
    );

    const self = {
        location,

        addEventListener: (type, handler) => (listeners[type] ??= []).push(handler),

        skipWaiting: () => {
            skips.count += 1;
        },

        registration: {
            active,
            update: () => {},
        },

        clients: {
            claim: async () => {
                claims.count += 1;
            },
            matchAll: async () =>
                clients.map((path) => ({
                    url: new URL(path, location).href,
                    // A client receives a copy of what a worker posted, not the worker's own object: the
                    // message is copied across the boundary, so the copy is what a reading compares against.
                    postMessage: (data) => {
                        messages.push({ to: new URL(path, location).href, data: structuredClone(data) });
                    },
                })),
        },
    };

    // The worker schedules its own update check; a test reads the delay it asked for rather than waiting
    // on a live timer.
    const setInterval = (fn, ms) => {
        intervals.push(ms);

        return 0;
    };

    const fetch = async (request) => {
        calls.push('network');

        const url = typeof request === 'string' ? new URL(request, location).href : request.url;

        if (unreachable.some((path) => new URL(path, location).href === url)) {
            throw new TypeError('network unreachable');
        }

        return { ok: true, status: 200, clone: () => ({}) };
    };

    const caches = {
        open: async (name) => ({
            addAll: async (names) => {
                for (const one of names) {
                    added.push(new URL(one, location).pathname);
                }
            },

            match: async (target) => {
                calls.push('cache');

                const url = typeof target === 'string' ? new URL(target, location).href : target.url;

                return holds.get(url);
            },

            put: async (request, response) => {
                holds.set(typeof request === 'string' ? new URL(request, location).href : request.url, response);
            },
        }),

        keys: async () => [...cacheNames],

        delete: async (name) => {
            deletions.push(name);
        },
    };

    const context = { self, setInterval, URL, Response, fetch, caches };

    vm.runInNewContext(workerSource(), context);

    return { listeners, calls, messages, deletions, intervals, claims, skips, added };
}

/** The worker source with the build's own manifest injected in place of the marker the build replaces. */
function workerSource() {
    const source = readFileSync(workerPath, 'utf8');

    assert.ok(source.includes('/* @precache-manifest */'), `${workerPath} carries no precache manifest for the build to fill`);

    return source.replace('/* @precache-manifest */', `const PRECACHE_MANIFEST = ${JSON.stringify(precached, null, 4)};`);
}

/** The cache name one worker source declares for itself. */
function cacheName(source) {
    const named = source.match(/const CACHE_NAME = '([^']+)'/);

    assert.ok(named, 'the worker source declares no cache name of its own');

    return named[1];
}

/**
 * ask hands the worker one fetch event and reports whether it answered, in what order it reached the
 * network and the cache, and what it finally handed back.
 */
async function ask(worker, path, { method = 'GET', mode } = {}) {
    worker.calls.length = 0;

    const url = path.startsWith('http') ? path : origin + path;
    const answered = [];

    for (const handler of worker.listeners.fetch ?? []) {
        handler({
            request: { method, mode: mode ?? (url.endsWith('/') ? 'navigate' : 'cors'), url },
            respondWith: (played) => answered.push(played),
        });
    }

    // The worker answers with a promise it fills by reaching the cache and the network, so what it handed
    // back and the order it reached them in are only known once that promise has run to the end.
    const settled = await Promise.allSettled(answered);

    return {
        intercepted: answered.length > 0,
        calls: worker.calls,
        answeredWith: settled.map((one) => (one.status === 'fulfilled' ? one.value?.tag : null)),
    };
}

/** fire runs every handler the worker registered for one event type, and waits for what it waited for. */
async function fire(worker, type, event = {}) {
    const waited = [];

    for (const handler of worker.listeners[type] ?? []) {
        handler({ ...event, waitUntil: (promise) => waited.push(promise) });
    }

    await Promise.allSettled(waited);
}

describe('admin/sw — what the worker stores and announces', () => {
    it('stores the names the build injected, and takes no page over while it stores them', async () => {
        const worker = loadWorker();

        await fire(worker, 'install');

        assert.deepEqual(worker.added, precached, 'the worker did not store the manifest the build injected');
        assert.equal(worker.skips.count, 0, 'the worker took a page over before it was asked to');
    });

    it('announces the new version only where a worker was already answering the page', async () => {
        const first = loadWorker({ clients: ['/admin/'] });

        await fire(first, 'install');

        assert.deepEqual(first.messages, [], 'a first install told the page about a version it already has');

        const again = loadWorker({ clients: ['/admin/'], active: { scriptURL: `${origin}/admin/sw.js` } });

        await fire(again, 'install');

        assert.deepEqual(
            again.messages.map((one) => one.data),
            [{ type: 'AppUpdateAvailable' }],
            'an install that replaced a controlling worker said nothing about it',
        );
    });

    it('announces it to the pages its own mount holds, and to no other page', async () => {
        const worker = loadWorker({
            clients: ['/admin/', '/admin/logs', '/'],
            active: { scriptURL: `${origin}/admin/sw.js` },
        });

        await fire(worker, 'install');

        const told = worker.messages.map((one) => new URL(one.to).pathname);

        assert.deepEqual(
            told,
            ['/admin/', '/admin/logs'],
            'the worker told a page the administration application does not hold, or left one it holds out',
        );
    });

    it('checks its own registration for a new version every five minutes', async () => {
        const worker = loadWorker();

        assert.deepEqual(worker.intervals, [300000], 'the worker schedules no update check on the interval it was given');
    });

    it('accepts the answer to skip waiting from one handler only', async () => {
        const worker = loadWorker();

        assert.equal(worker.listeners.message?.length, 1, 'the worker registered a message handler more than once');

        await fire(worker, 'message', { data: { type: 'SKIP_WAITING' } });

        assert.equal(worker.skips.count, 1, 'the handler that answers SKIP_WAITING did not skip waiting');

        await fire(worker, 'message', { data: { type: 'SomethingElse' } });

        assert.equal(worker.skips.count, 1, 'a message that was not the answer took a page over anyway');
    });
});

describe('admin/sw — the requests it leaves alone', () => {
    it('leaves the server calls the administration page makes, and the socket behind them, to the server', async () => {
        const worker = loadWorker();

        for (const path of ['/api/admin/config', '/api/admin/login', '/api/stream']) {
            assert.equal((await ask(worker, path)).intercepted, false, `${path} was intercepted`);
        }
    });

    it('leaves another origin alone', async () => {
        const worker = loadWorker();

        assert.equal((await ask(worker, 'https://other.example/admin/main.AAA111.js')).intercepted, false);
    });

    it('leaves a request that is not a GET alone', async () => {
        const worker = loadWorker();

        assert.equal((await ask(worker, '/admin/', { method: 'POST' })).intercepted, false);
    });
});

describe('admin/sw — the requests it answers', () => {
    it('answers a navigation from the network, and from the stored shell when the network does not answer', async () => {
        const served = loadWorker({ stored: ['/admin/index.html'] });

        for (const path of ['/admin/', '/admin/systems/edit']) {
            const { intercepted, calls, answeredWith } = await ask(served, path, { mode: 'navigate' });

            assert.equal(intercepted, true, `${path} was not answered`);
            assert.equal(calls[0], 'network', `${path} did not go to the network first`);
            assert.equal(answeredWith[0], undefined, `${path} was answered from the cache while the server answered`);
        }

        const offline = loadWorker({ stored: ['/admin/index.html'], unreachable: ['/admin/systems/edit'] });

        const { intercepted, answeredWith } = await ask(offline, '/admin/systems/edit', { mode: 'navigate' });

        assert.equal(intercepted, true, 'a navigation the network refused was not answered at all');
        assert.deepEqual(
            answeredWith,
            ['/admin/index.html'],
            'the shell the worker fell back to is not the shell of the application it serves',
        );
    });

    it('answers the module, stylesheet, and copied assets of its own bundle from the cache', async () => {
        const worker = loadWorker();

        for (const path of [
            '/admin/main.AAA1111.js',
            '/admin/main.BBB2222.css',
            '/admin/assets/fonts/roboto-latin-w400.woff2',
            '/admin/assets/icons/icon-192x192.png',
        ]) {
            const { intercepted, calls } = await ask(worker, path);

            assert.equal(intercepted, true, `${path} was not answered`);
            assert.equal(calls[0], 'cache', `${path} did not go to the cache first`);
        }
    });

    it('answers a name under its mount that its build does not emit from the network', async () => {
        const worker = loadWorker();

        for (const path of ['/admin/openapi.json', '/admin/chunk.AAA1111.js', '/admin/media/logo.png']) {
            const { intercepted, calls } = await ask(worker, path);

            assert.equal(intercepted, true, `${path} was not answered`);
            assert.equal(calls[0], 'network', `${path} was treated as a cached asset`);
        }
    });

    it('resolves every path it looks up against where it sits, naming no mount in its own source', async () => {
        const source = readFileSync(workerPath, 'utf8');

        assert.ok(
            !source.includes('/admin'),
            'the worker names the mount it is served from instead of resolving it from its own location',
        );
    });
});

describe('admin/sw — the caches it leaves on the origin', () => {
    it('keeps its own cache and the listener application cache, and deletes every other name', async () => {
        const worker = loadWorker({
            cacheNames: ['rdio-admin-static', 'rdio-scanner-static', 'rdio-admin-static.v7'],
        });

        await fire(worker, 'activate');

        assert.deepEqual(worker.deletions, ['rdio-admin-static.v7'], 'the worker deleted a cache it does not own, or kept one nobody owns');
    });

    it('takes over the pages its own scope covers when it activates', async () => {
        const worker = loadWorker({ cacheNames: ['rdio-admin-static'] });

        await fire(worker, 'activate');

        assert.equal(worker.claims.count, 1, 'the worker activated without claiming the pages it answers');
    });

    it('names a cache of its own, and names the one it must leave', () => {
        const own = cacheName(readFileSync(workerPath, 'utf8'));
        const other = cacheName(readFileSync(scannerWorkerPath, 'utf8'));

        assert.notEqual(own, other, 'both applications write the same cache, so one activation erases the other');
        assert.ok(readFileSync(workerPath, 'utf8').includes(other), 'the administration worker does not name the cache it must leave');
    });
});
