// tests for the scanner service worker: the paths the worker leaves alone
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import { describe, it } from 'node:test';

const here = dirname(fileURLToPath(import.meta.url));
const origin = 'https://rdio.example';
const workerPath = join(here, '..', 'infrastructure', 'service-worker.js');

/** The names the scanner build precaches, injected the way the build injects them. */
const manifestSource = () =>
    readFileSync(workerPath, 'utf8').replace(
        '/* @precache-manifest */',
        `const PRECACHE_MANIFEST = ${JSON.stringify(
            ['/index.html', '/manifest.webmanifest', '/sw.js', '/favicon.ico', '/main.AAA1111.js', '/main.BBB2222.css'],
            null,
            4,
        )};`,
    );

/**
 * The worker is a script for a browser's worker global scope, not a module: it has no exports, and the
 * build injects its precache manifest into it. So the test runs the source against a stand-in for that
 * scope, keeps the listeners the worker registered, and then asks it the questions a browser would.
 *
 * `calls` records the order the worker reached for the network and for the cache, which is the only
 * visible difference between the two strategies the fetch handler has.
 *
 * `clients` and `cacheNames` name the pages the worker was told about and the caches already on the
 * origin, because what the worker does with those is a reading about which pages and which caches belong
 * to the other application.
 */
function loadWorker({ clients = [], cacheNames = [], active = null } = {}) {
    const listeners = {};
    const calls = [];
    const announcements = [];
    const deletions = [];

    const context = {
        self: {
            location: { origin },
            addEventListener: (type, handler) => (listeners[type] ??= []).push(handler),
            registration: { active, update: () => {} },
            skipWaiting: () => {},
            clients: {
                claim: async () => {},
                matchAll: async () =>
                    clients.map((path) => ({
                        url: origin + path,
                        postMessage: (data) => announcements.push({ to: origin + path, data }),
                    })),
            },
        },

        // The worker schedules its own update check; a test does not need a live timer for it.
        setInterval: () => 0,

        URL,
        Response,

        fetch: async (request) => {
            calls.push('network');

            return { ok: true, status: 200, clone: () => ({}) };
        },

        caches: {
            open: async () => ({
                addAll: async () => {},
                match: async () => {
                    calls.push('cache');

                    return undefined;
                },
                put: async () => {},
            }),
            keys: async () => [...cacheNames],
            delete: async (name) => {
                deletions.push(name);
            },
        },
    };

    vm.runInNewContext(manifestSource(), context);

    return { listeners, calls, announcements, deletions };
}

/** ask hands the worker one fetch event and reports whether it answered, and in what order. */
async function ask(worker, path, { method = 'GET', mode } = {}) {
    worker.calls.length = 0;

    const url = path.startsWith('http') ? path : origin + path;
    const answered = [];

    for (const handler of worker.listeners.fetch) {
        handler({
            request: { method, mode: mode ?? (url.endsWith('/') ? 'navigate' : 'cors'), url },
            respondWith: (played) => answered.push(played),
        });
    }

    // The worker answers with a promise it fills by reaching the cache and the network, so the order
    // it reached them in is only known once that promise has run to the end.
    await Promise.allSettled(answered);

    return { intercepted: answered.length > 0, calls: worker.calls };
}

/** fire runs every handler the worker registered for one event, and waits for what it waited for. */
async function fire(worker, type, event = {}) {
    const waited = [];

    for (const handler of worker.listeners[type] ?? []) {
        handler({ ...event, waitUntil: (promise) => waited.push(promise) });
    }

    await Promise.allSettled(waited);
}

describe('scanner/sw — the paths the worker leaves alone', async () => {
    it('does not intercept the administration application or anything under it', async () => {
        const worker = loadWorker();

        for (const path of ['/admin', '/admin/', '/admin/systems/edit', '/admin/main.AAA111.css', '/admin/assets/fonts/roboto-latin-w400.woff2']) {
            assert.equal((await ask(worker, path)).intercepted, false, `${path} was intercepted by the scanner worker`);
        }
    });

    it('does not intercept an API call or the sockets behind it', async () => {
        const worker = loadWorker();

        for (const path of ['/api/admin/config', '/api/admin/login', '/api/scan']) {
            assert.equal((await ask(worker, path)).intercepted, false, `${path} was intercepted`);
        }
    });

    it('does not intercept another origin', async () => {
        const worker = loadWorker();

        assert.equal((await ask(worker, 'https://other.example/main.AAA111.js')).intercepted, false);
    });

    it('does not intercept a request that is not a GET', async () => {
        const worker = loadWorker();

        assert.equal((await ask(worker, '/index.html', { method: 'POST' })).intercepted, false);
    });
});

describe('scanner/sw — the paths the worker answers', async () => {
    it('answers a navigation from the network, falling back to the stored shell', async () => {
        const worker = loadWorker();

        for (const path of ['/', '/some/route']) {
            const { intercepted, calls } = await ask(worker, path);

            assert.equal(intercepted, true, `${path} was not answered`);
            assert.equal(calls[0], 'network', `${path} did not go to the network first`);
        }
    });

    it('answers the names the scanner build emits from the cache', async () => {
        const worker = loadWorker();

        for (const path of ['/main.AAA111.js', '/main.BBB222.css', '/assets/fonts/roboto-latin-w400.woff2', '/assets/icons/icon-192x192.png']) {
            const { intercepted, calls } = await ask(worker, path);

            assert.equal(intercepted, true, `${path} was not answered`);
            assert.equal(calls[0], 'cache', `${path} did not go to the cache first`);
        }
    });

    it('answers a name the build no longer emits from the network, not from the cache', async () => {
        const worker = loadWorker();

        for (const path of ['/chunk.AAA111.js', '/listener.AAA111.js', '/media/logo.png']) {
            const { intercepted, calls } = await ask(worker, path);

            assert.equal(intercepted, true, `${path} was not answered`);
            assert.equal(calls[0], 'network', `${path} was treated as a cached asset`);
        }
    });
});

describe('scanner/sw — the update it announces', () => {
    it('registers the handler that answers the accepted update once', () => {
        // The name every version of the worker shares cannot tell one registration of a handler from two,
        // so the count is read where it is written.
        const handlers = readFileSync(workerPath, 'utf8').match(/addEventListener\('message'/g) ?? [];

        assert.equal(
            handlers.length,
            1,
            `the worker registers a message handler ${handlers.length} times, and only one of them is asked for`,
        );
    });

    it("announces the new version to the pages it serves, and not to the administration application's page", async () => {
        const worker = loadWorker({
            clients: ['/', '/logs', '/admin/', '/admin/systems/edit'],
            active: { scriptURL: `${origin}/sw.js` },
        });

        await fire(worker, 'install');

        assert.deepEqual(
            worker.announcements.map((one) => new URL(one.to).pathname),
            ['/', '/logs'],
            'the worker told a page the administration application holds, which is a question the operator was never asked to answer there',
        );
    });
});

describe('scanner/sw — the caches it leaves on the origin', () => {
    it('keeps the administration application cache and its own, and deletes every other name', async () => {
        const worker = loadWorker({
            cacheNames: ['rdio-scanner-static', 'rdio-admin-static', 'rdio-scanner-static.old'],
        });

        await fire(worker, 'activate');

        assert.deepEqual(
            worker.deletions,
            ['rdio-scanner-static.old'],
            `the worker deleted ${JSON.stringify(worker.deletions)} of the caches it was asked to leave on the origin`,
        );
    });
});
