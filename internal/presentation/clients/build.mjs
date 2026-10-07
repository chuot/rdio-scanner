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
 * Contract: docs/specs/FEAT-002-vanilla-client-ng/contract-client-ng.md §1-§2, §6.
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
const scannerDir = join(here, 'scanner');
const adminDir = join(here, 'admin');
const sharedDir = join(here, 'shared');

/**
 * The dispatcher that dynamically imports exactly one page entry. It is deleted when each
 * application gets its own entry; until then it is the entry point of the single bundle.
 */
const dispatcher = join(here, 'src', 'main.js');

const args = process.argv.slice(2);
const production = args.includes('--production');
const watch = args.includes('--watch');

const packageInfo = JSON.parse(readFileSync(join(here, 'package.json'), 'utf8'));

/**
 * The directory a config writes to: the bundle of the Go package that embeds this client.
 *
 * There was a `--out <dir>` flag here while the repository carried two servers, because the same client had to
 * be able to feed the legacy `server/webapp/` and the new one. There is one server now, each client sits inside
 * its presentation layer, and the destination is a fact about where the application lives rather than a choice
 * made per build. Passing the old flag is refused rather than silently ignored: a build that looks like it
 * targets somewhere else should fail loudly instead of writing into the bundle anyway.
 */
export function resolveOutDir(argv, from = here) {
    const taken = argv.find((arg) => arg === '--out' || arg.startsWith('--out='));

    if (taken !== undefined) {
        throw new Error(
            'build: --out is gone — there is one server, and this client builds into ../webapp/bundle'
        );
    }

    return resolve(from, '..', 'webapp', 'bundle');
}

const bundleDir = resolveOutDir(args);

/**
 * The listener application. Until the two applications are split, its entry is the
 * dispatcher that dynamically imports either the listener or the admin page, which is
 * what today's single bundle is made of.
 */
export const scannerConfig = {
    name: 'scanner',
    srcDir: scannerDir,
    entryPoints: [dispatcher],
    indexHtml: join(scannerDir, 'index.html'),
    serviceWorker: { source: join(scannerDir, 'sw.js'), register: 'sw.js', scope: '/' },
    staticFiles: [join(sharedDir, 'favicon.ico'), join(scannerDir, 'manifest.webmanifest')],
    staticDirs: [join(sharedDir, 'assets')],
    outDir: bundleDir,
};

/**
 * The admin application. Declared now so the two output directories are visible in one
 * place; it starts building when its own sources and shell exist. It has no service
 * worker and no offline cache.
 */
export const adminConfig = {
    name: 'admin',
    srcDir: adminDir,
    entryPoints: [join(adminDir, 'admin.js')],
    indexHtml: join(adminDir, 'index.html'),
    serviceWorker: null,
    staticFiles: [join(sharedDir, 'favicon.ico'), join(adminDir, 'manifest.webmanifest')],
    staticDirs: [join(sharedDir, 'assets')],
    outDir: bundleDir,
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
        cpSync(dir, join(config.outDir, basename(dir)), { recursive: true });
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
 * Emit sw.js with the generated precache manifest injected as a constant
 * (decision-pwa-custom-sw.md §4). Stable, unhashed filename.
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
        const copied = basename(dir);
        if (existsSync(join(config.outDir, copied))) walk(copied);
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
            __RDIO_VERSION__: JSON.stringify(packageInfo.version),
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
    console.log(`[${config.name}] built v${packageInfo.version} into ${config.outDir}\n${sizes}`);

    return result;
}

// The build runs when this file is run. A test imports resolveOutDir and the two configs from it, and
// importing it must not rebuild the webapp.
const runThisBuild = process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url);

if (runThisBuild) {
    await buildApp(scannerConfig);
}
