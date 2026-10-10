import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import vm from 'node:vm';
import { describe, it } from 'node:test';

const here = dirname(fileURLToPath(import.meta.url));
const origin = 'https://rdio.example';

/**
 * The worker is a script for a browser's worker global scope, not a module: it has no exports, and the
 * build injects its precache manifest into it. So the test runs the source against a stand-in for that
 * scope, keeps the listeners the worker registered, and then asks it the questions a browser would.
 *
 * `calls` records the order the worker reached for the network and for the cache, which is the only
 * visible difference between the two strategies the fetch handler has.
 */
function loadWorker() {
    const listeners = {};
    const calls = [];

    const context = {
        self: {
            location: { origin },
            addEventListener: (type, handler) => (listeners[type] ??= []).push(handler),
            registration: { update: () => {} },
            clients: { claim: async () => {}, matchAll: async () => [] },
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
            keys: async () => [],
            delete: async () => {},
        },
    };

    vm.runInNewContext(readFileSync(join(here, '..', 'infrastructure', 'service-worker.js'), 'utf8'), context);

    return { listeners, calls };
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
