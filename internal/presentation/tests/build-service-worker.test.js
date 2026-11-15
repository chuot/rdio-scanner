// tests for the build's service worker: what the build generates for each application
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { after, describe, it } from 'node:test';

import * as build from '../build.mjs';

// The build generates two pieces of text for an application that has a worker: the precache manifest it
// injects into the worker source, and the registration statement the built shell carries. Both are read
// here as text, from the config the build itself uses, so the mount each application is served from is
// what decides the names — not a string written beside it.
//
// The manifest names the files a build emits and the asset trees it copies into the bundle, and it reads
// the copied names out of the bundle directory. The two applications are therefore read against a bundle
// directory written here: which files belong in a bundle is fixed by the reading rather than left to
// whatever an earlier build happened to leave on disk.
const bundleDir = mkdtempSync(join(tmpdir(), 'rdio-build-bundle-'));

for (const asset of ['assets/fonts/roboto-latin-w400.woff2', 'assets/icons/icon-192x192.png']) {
    mkdirSync(join(bundleDir, join(...asset.split('/').slice(0, -1))), { recursive: true });
    writeFileSync(join(bundleDir, asset), 'asset');
}

after(() => rmSync(bundleDir, { recursive: true, force: true }));

// What one build of either application emits: a hashed entry module and a hashed stylesheet.
const emitted = ['main.AAA1111.js', 'main.BBB2222.css'];

const production = true;
const development = false;

// Each application's own config, read against the bundle directory above.
const scanner = { ...build.scannerConfig, outDir: bundleDir };
const admin = { ...build.adminConfig, outDir: bundleDir };
const configs = [scanner, admin];

/** A config like the administration one, with the worker it does not have taken away. */
const withoutWorker = { ...admin, serviceWorker: null };

/** The worker entry an application config carries, or the reason it does not. */
function workerEntry(config) {
    assert.ok(config.serviceWorker, `${config.name} config carries no service worker to build`);

    return config.serviceWorker;
}

/**
 * The paths the generated manifest names, in the order it names them.
 *
 * The manifest is read as the array of paths it holds rather than as the exact bytes it is written in:
 * the paths and their order are the claim, and the spacing around them is the build's own layout.
 */
function precached(config, { names = emitted, isProduction = production } = {}) {
    assert.equal(
        typeof build.serviceWorkerManifest,
        'function',
        'the build generates no precache manifest that a test can read',
    );

    const text = build.serviceWorkerManifest(config, names, isProduction);

    if (text === null || text === undefined) {
        return null;
    }

    const literal = text.match(/\[[\s\S]*?\]/);

    assert.ok(literal, `the manifest is not an array of paths: ${JSON.stringify(text)}`);

    return JSON.parse(literal[0]);
}

describe('build.mjs — the worker each application config carries', () => {
    it('gives each application a worker source of its own', () => {
        for (const config of [build.scannerConfig, build.adminConfig]) {
            const source = workerEntry(config).source;

            assert.ok(
                source.startsWith(config.srcDir),
                `${config.name} builds its worker from ${source}, which is not inside its own source directory`,
            );
        }
    });

    it('names each worker by the file the built shell registers, under the mount it is served from', () => {
        const pairs = [build.scannerConfig, build.adminConfig].map((config) => ({
            register: workerEntry(config).register,
            scope: workerEntry(config).scope,
        }));

        assert.deepEqual(
            pairs,
            [
                { register: 'sw.js', scope: '/' },
                { register: 'sw.js', scope: '/admin/' },
            ],
            'one of the two applications registers its worker somewhere other than the mount it is served from',
        );
    });

    it('adds no mount string beside the scope the config already carries', () => {
        // The mount a precached name is rooted at is the registration scope read another way, so a second
        // entry naming it would be a second fact to keep in step.
        for (const config of configs) {
            assert.deepEqual(
                Object.keys(workerEntry(config)).sort(),
                ['register', 'scope', 'source'],
                `${config.name} carries a service worker entry with more than its source, its registration name, and its scope`,
            );
        }
    });
});

describe('build.mjs — the manifest the build injects into each worker', () => {
    it('roots every administration precached name at the administration mount', () => {
        const entries = precached(admin);

        assert.ok(entries, 'the build generates no manifest for the administration application');

        for (const entry of entries) {
            assert.ok(
                entry.startsWith('/admin/'),
                `${JSON.stringify(entry)} is precached outside the mount the administration application is served from`,
            );
        }
    });

    it('precaches the administration shell, its own manifest, its worker, its favicon, its emitted module and stylesheet, and its copied assets', () => {
        assert.deepEqual(
            precached(admin),
            [
                '/admin/index.html',
                '/admin/manifest.webmanifest',
                '/admin/sw.js',
                '/admin/favicon.ico',
                '/admin/main.AAA1111.js',
                '/admin/main.BBB2222.css',
                '/admin/assets/fonts/roboto-latin-w400.woff2',
                '/admin/assets/icons/icon-192x192.png',
            ],
            'the administration manifest does not name the files the administration bundle serves',
        );
    });

    it('keeps every scanner precached name at the origin root, entry for entry', () => {
        // The shipped scanner manifest is rooted at the origin, and the listener is served there. Deriving
        // the prefix has to leave that manifest holding exactly the names it holds today.
        assert.deepEqual(
            precached(scanner),
            [
                '/index.html',
                '/manifest.webmanifest',
                '/sw.js',
                '/favicon.ico',
                '/main.AAA1111.js',
                '/main.BBB2222.css',
                '/assets/fonts/roboto-latin-w400.woff2',
                '/assets/icons/icon-192x192.png',
            ],
            'the scanner manifest no longer holds the names the shipped build precaches',
        );
    });

    it('names no path relative to the manifest in either application', () => {
        // A relative entry resolves against the worker's own location, which is the right directory and a
        // different shape from the one both manifests hold today.
        for (const config of configs) {
            for (const entry of precached(config) ?? []) {
                assert.ok(
                    entry.startsWith('/'),
                    `${JSON.stringify(entry)} is not rooted at the mount the worker is served from`,
                );
            }
        }
    });

    it('generates no manifest for an application with no worker, and none for a development build', () => {
        assert.equal(
            precached(withoutWorker),
            null,
            'a config with no service worker generated a manifest anyway',
        );

        for (const config of configs) {
            assert.equal(
                precached(config, { isProduction: development }),
                null,
                `${config.name} generated a manifest for a development build`,
            );
        }
    });
});

describe('build.mjs — the registration statement the built shell carries', () => {
    /** The statement the build writes into the shell, or nothing when it writes none. */
    function registration(config, isProduction) {
        assert.equal(
            typeof build.serviceWorkerRegistration,
            'function',
            'the build generates no registration statement that a test can read',
        );

        return build.serviceWorkerRegistration(config, isProduction);
    }

    it('registers the worker the shell names, at the scope the config names', () => {
        const administration = registration(admin, production);
        const listener = registration(scanner, production);

        assert.ok(administration, 'the build generates no registration statement for the administration application');
        assert.ok(listener, 'the build generates no registration statement for the scanner');

        assert.ok(
            administration.includes(`navigator.serviceWorker.register('sw.js', { scope: '/admin/' })`),
            `the administration shell registers something other than its own worker at /admin/: ${administration}`,
        );

        assert.ok(
            listener.includes(`navigator.serviceWorker.register('sw.js', { scope: '/' })`),
            `the scanner shell registers something other than its own worker at /: ${listener}`,
        );
    });

    it('writes no registration statement for an application with no worker, and none for a development build', () => {
        assert.equal(
            registration(withoutWorker, production),
            null,
            'a config with no service worker generated a registration statement anyway',
        );

        for (const config of configs) {
            assert.equal(
                registration(config, development),
                null,
                `${config.name} registered a worker in a development build`,
            );
        }
    });
});
