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
 * Client build script (esbuild, single dev dependency).
 *
 * One `buildApp(config)` and one config per application. A config names its source
 * directory, its entry points, its HTML shell, its service worker (or none), its
 * static copies, and its output directory. The output directory is always a relative
 * path inside the presentation layer, beside the Go package that embeds it, so there
 * is nothing for a flag to redirect it to.
 */

import { cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { basename, dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import esbuild from 'esbuild';

const here = dirname(fileURLToPath(import.meta.url));
const scannerDir = join(here, 'scanner', 'frontend');
const adminDir = join(here, 'admin', 'frontend');
const sharedDir = join(here, 'common', 'frontend');

const args = process.argv.slice(2);
const production = args.includes('--production');
const watch = args.includes('--watch');

/** The repository root, two directories above the presentation directory this file sits in. */
const repoRoot = resolve(here, '..', '..');

/** A semantic-versioning core and nothing else: the same rule `cmd/build/buildinfo` applies to the file. */
const corePattern = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/;

/**
 * The version a bundle carries, resolved in the order the two sources are worth asking.
 *
 * 1. the version the build tool named, in `RDIO_VERSION`, which is the only one that knows whether this
 *    build is a prerelease and which commit it came from;
 * 2. the core the repository declared in `VERSION.txt`, so `npm run build` on its own still produces a
 *    sane version rather than a made-up one;
 * 3. `dev`, which is a version no release ever had and which nobody mistakes for one.
 *
 * `package.json` is not one of the sources. It carried a version nobody maintained, and a manifest that
 * is private and unpublished has no reason to hold one.
 *
 * A declared version that is not a bare core is refused rather than repaired: a bundle that reported
 * `8.0.0` because somebody wrote `v8.0.0` would be reporting a version the repository never wrote down.
 */
export function resolveVersion(env, root) {
    const named = env && env.RDIO_VERSION;

    if (typeof named === 'string' && named.trim() !== '') {
        return named.trim();
    }

    let contents;

    try {
        contents = readFileSync(join(root, 'VERSION.txt'), 'utf8');
    } catch (err) {
        if (err.code === 'ENOENT') {
            return 'dev';
        }

        throw err;
    }

    const core = contents.replace(/^\uFEFF/, '').trim();

    if (!corePattern.test(core)) {
        throw new Error(`build: VERSION.txt must hold one bare core version in the form X.Y.Z, and holds ${JSON.stringify(core)}`);
    }

    return core;
}

/** The version this build carries, resolved once and given to every bundle the build produces. */
const version = resolveVersion(process.env, repoRoot);

/**
 * The directory an application writes to: the bundle of the Go package that embeds it.
 *
 * There was a `--out <dir>` flag here while the repository carried two servers, because the same client had to
 * be able to feed the legacy `server/webapp/` and the new one. There is one server now, each application sits
 * inside its presentation layer, and the destination is a fact about where the application lives rather than a
 * choice made per build. Passing the old flag is refused rather than silently ignored: a build that looks like
 * it targets somewhere else should fail loudly instead of writing into a bundle anyway.
 */
export function resolveOutDir(bundlePath, argv) {
    const taken = argv.find((arg) => arg === '--out' || arg.startsWith('--out='));

    if (taken !== undefined) {
        throw new Error(
            'build: --out is gone — each application builds into the bundle beside the Go package that embeds it'
        );
    }

    return resolve(here, bundlePath);
}

/** The scanner bundle sits beside the Go package that embeds it. */
const scannerBundle = resolveOutDir('scanner/frontend/bundle', args);

/** The admin bundle sits beside the Go package that embeds it. */
const adminBundle = resolveOutDir('admin/frontend/bundle', args);

/**
 * The asset directories a bundle carries, named one by one instead of copying `assets` wholesale.
 *
 * `assets/` holds two kinds of file, and a recursive copy cannot tell them apart. Fonts and icons are URLs
 * that `external` leaves in the emitted CSS and manifest, so the build has to copy the files. Badges are
 * imported by `rdio-promo.js` with a relative path, `loader: { '.svg': 'text' }` inlines their markup into
 * the JS, and a copy of them in a bundle would be bytes no request ever fetches. Naming the subdirectories
 * is what stops a badge added under `assets/` from being copied along with everything else.
 */
const copiedAssets = [
    { from: join(sharedDir, 'assets', 'fonts'), to: 'assets/fonts' },
    { from: join(sharedDir, 'assets', 'icons'), to: 'assets/icons' },
];

/** The scanner application: the listener page, its service worker, and its manifest. */
export const scannerConfig = {
    name: 'scanner',
    srcDir: scannerDir,
    entryPoints: [join(scannerDir, 'main.js')],
    indexHtml: join(scannerDir, 'index.html'),
    serviceWorker: { source: join(scannerDir, 'infrastructure', 'service-worker.js'), register: 'sw.js', scope: '/' },
    staticFiles: [join(sharedDir, 'favicon.ico'), join(scannerDir, 'manifest.webmanifest')],
    staticDirs: copiedAssets,
    outDir: scannerBundle,
};

/** The admin application: no service worker, no offline cache, its own shell and manifest. */
export const adminConfig = {
    name: 'admin',
    srcDir: adminDir,
    entryPoints: [join(adminDir, 'main.js')],
    indexHtml: join(adminDir, 'index.html'),
    serviceWorker: null,
    staticFiles: [join(sharedDir, 'favicon.ico'), join(adminDir, 'manifest.webmanifest')],
    staticDirs: copiedAssets,
    outDir: adminBundle,
};

function cleanOutdir(config) {
    // The output directory is a tracked directory in the repository: the server keeps a placeholder in it so
    // its binary compiles before the client has ever been built. Cleaning the built files must not delete
    // that placeholder.
    const placeholder = existsSync(join(config.outDir, '.gitkeep')) ? readFileSync(join(config.outDir, '.gitkeep')) : null;

    rmSync(config.outDir, { recursive: true, force: true });
    mkdirSync(config.outDir, { recursive: true });

    if (placeholder) {
        writeFileSync(join(config.outDir, '.gitkeep'), placeholder);
    }
}

function copyStatic(config) {
    for (const file of config.staticFiles) {
        cpSync(file, join(config.outDir, basename(file)));
    }
    for (const dir of config.staticDirs) {
        cpSync(dir.from, join(config.outDir, dir.to), { recursive: true });
    }
}

/** Metafile output keys are paths relative to cwd; return the bundle-relative name. */
function outName(config, key) {
    return relative(config.outDir, resolve(here, key)).split('\\').join('/');
}

function listOutputs(config, metafile) {
    return Object.keys(metafile ? metafile.outputs : {}).map((key) => outName(config, key));
}

/**
 * Inject the hashed entry script + stylesheet links into index.html and, in
 * production, the service worker registration bootstrap.
 */
function processIndexHtml(config, names) {
    let html = readFileSync(config.indexHtml, 'utf8');

    const mainJs = names.find((n) => /(^|\/)main(\.[A-Za-z0-9]+)?\.js$/.test(n));
    const css = names.find((n) => n.endsWith('.css'));

    if (!mainJs) {
        throw new Error(`build: ${config.name} entry output not found in metafile`);
    }

    const head = [];

    if (css) {
        head.push(`<link rel="stylesheet" href="${css}">`);
    }

    const body = [];
    body.push(`<script type="module" src="${mainJs}"></script>`);

    if (production && config.serviceWorker) {
        body.push(`<script type="module">
if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('${config.serviceWorker.register}', { scope: '${config.serviceWorker.scope}' }).catch(() => {});
}
</script>`);
    }

    html = html.replace('<!-- @app-head -->', head.join('\n        '));
    html = html.replace('<!-- @app-body -->', body.join('\n    '));

    writeFileSync(join(config.outDir, 'index.html'), html);
}

/**
 * Emit the service worker with the generated precache manifest injected as a
 * constant (decision-pwa-custom-sw.md §4). The source lives with the other
 * infrastructure modules; the name it is written under is the one the built
 * page registers, and it stays unhashed.
 */
function processServiceWorker(config, names) {
    if (!config.serviceWorker) {
        return;
    }

    const hashed = names.filter((n) => n.endsWith('.js') || n.endsWith('.css'));

    const precache = [
        '/index.html',
        '/manifest.webmanifest',
        '/sw.js',
        '/favicon.ico',
        ...hashed.map((n) => '/' + n),
        ...listStaticAssets(config).map((p) => '/' + p),
    ];

    let sw = readFileSync(config.serviceWorker.source, 'utf8');
    sw = sw.replace(
        '/* @precache-manifest */',
        'const PRECACHE_MANIFEST = ' + JSON.stringify(precache, null, 4) + ';',
    );

    writeFileSync(join(config.outDir, 'sw.js'), sw);
}

function listStaticAssets(config) {
    const files = [];
    const walk = (dir) => {
        for (const entry of readdirSync(join(config.outDir, dir), { withFileTypes: true })) {
            const p = join(dir, entry.name);
            if (entry.isDirectory()) walk(p);
            else files.push(p.split('\\').join('/'));
        }
    };
    for (const dir of config.staticDirs) {
        if (existsSync(join(config.outDir, dir.to))) walk(dir.to);
    }
    return files.sort();
}

function postProcess(config, result) {
    const names = listOutputs(config, result.metafile);
    processIndexHtml(config, names);
    processServiceWorker(config, names);
}

function buildOptions(config) {
    return {
        bundle: true,
        define: {
            __RDIO_VERSION__: JSON.stringify(version),
        },
        entryNames: production ? '[name].[hash]' : '[name]',
        entryPoints: config.entryPoints,
        // Both spellings of an asset URL are left exactly as written. The absolute form is what the
        // listener has always emitted; the relative form is what the shared stylesheet now uses, so the
        // same CSS resolves against whichever bundle serves it. Without the relative pattern esbuild
        // would try to open `assets/fonts/…` from the stylesheet's own directory and fail the build.
        external: ['/assets/*', 'assets/*'],
        format: 'esm',
        legalComments: 'none',
        loader: {
            '.svg': 'text',
        },
        metafile: true,
        minify: production,
        outdir: config.outDir,
        sourcemap: !production,
        splitting: true,
        target: ['es2022'],
        write: true,
    };
}

/** Build one application: clean its bundle, run esbuild, copy its static files, inject the shell. */
async function buildApp(config) {
    cleanOutdir(config);

    if (watch) {
        const context = await esbuild.context(buildOptions(config));
        copyStatic(config);
        postProcess(config, await context.rebuild());
        console.log(`[${config.name}] watching for changes…`);
        context.watch();
        return null;
    }

    copyStatic(config);
    const result = await esbuild.build(buildOptions(config));
    postProcess(config, result);

    const sizes = Object.entries(result.metafile.outputs)
        .map(([p, o]) => `  ${outName(config, p)}  ${(o.bytes / 1024).toFixed(1)} KB`)
        .join('\n');
    console.log(`[${config.name}] built v${version} into ${config.outDir}\n${sizes}`);

    return result;
}

// The build runs when this file is run. A test imports resolveOutDir and the two configs from it, and
// importing it must not rebuild the webapp.
const runThisBuild = process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url);

// `--scanner` or `--admin` builds one application; with neither, the build produces both. The Go build
// tool runs one application per step, so each bundle is built by the step that names it and the run says
// which one it built.
const only = args.includes('--scanner') ? 'scanner' : args.includes('--admin') ? 'admin' : null;

if (runThisBuild) {
    if (only !== 'admin') {
        await buildApp(scannerConfig);
    }
    if (only !== 'scanner') {
        await buildApp(adminConfig);
    }
}
