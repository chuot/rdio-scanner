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
 * Output: ../webapp/bundle/ — flat, content-hashed (production), consumable by
 * //go:embed all:bundle with SPA fallback. The client lives beside the Go package
 * that embeds it, so that output is a relative path inside the presentation layer
 * and there is nothing left for a flag to redirect it to.
 */

import { cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { basename, dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import esbuild from 'esbuild';

const here = dirname(fileURLToPath(import.meta.url));
const srcDir = join(here, 'src');

const args = process.argv.slice(2);
const production = args.includes('--production');
const watch = args.includes('--watch');

/**
 * The directory the build writes to: the bundle of the Go package that embeds this client, one directory up.
 *
 * There was a `--out <dir>` flag here while the repository carried two servers, because the same client had to
 * be able to feed the legacy `server/webapp/` and the new one. There is one server now, the client sits inside
 * its presentation layer, and the destination is a fact about where the client lives rather than a choice made
 * per build. Passing the old flag is refused rather than silently ignored: a build that looks like it targets
 * somewhere else should fail loudly instead of writing into the bundle anyway.
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

const outDir = resolveOutDir(args);

const packageInfo = JSON.parse(readFileSync(join(here, 'package.json'), 'utf8'));

/** Files copied verbatim into the webapp root. */
const staticFiles = ['favicon.ico', 'manifest.webmanifest'];

/** Directories copied verbatim (assets/icons, assets/fonts). */
const staticDirs = ['assets'];

function cleanOutdir() {
    // The output directory is a tracked directory in the repository: the server keeps a placeholder in it so
    // its binary compiles before the client has ever been built. Cleaning the built files must not delete
    // that placeholder.
    const placeholder = existsSync(join(outDir, '.gitkeep')) ? readFileSync(join(outDir, '.gitkeep')) : null;

    rmSync(outDir, { recursive: true, force: true });
    mkdirSync(outDir, { recursive: true });

    if (placeholder) {
        writeFileSync(join(outDir, '.gitkeep'), placeholder);
    }
}

function copyStatic() {
    for (const file of staticFiles) {
        cpSync(join(srcDir, file), join(outDir, file));
    }
    for (const dir of staticDirs) {
        cpSync(join(srcDir, dir), join(outDir, dir), { recursive: true });
    }
}

/** Metafile output keys are paths relative to cwd; return the webapp-relative name. */
function outName(key) {
    return relative(outDir, resolve(here, key)).split('\\').join('/');
}

function listOutputs(metafile) {
    return Object.keys(metafile ? metafile.outputs : {}).map(outName);
}

/**
 * Inject the hashed entry script + stylesheet links into index.html and, in
 * production, the service worker registration bootstrap.
 */
function processIndexHtml(names) {
    let html = readFileSync(join(srcDir, 'index.html'), 'utf8');

    const mainJs = names.find((n) => /(^|\/)main(\.[A-Za-z0-9]+)?\.js$/.test(n));
    const css = names.find((n) => n.endsWith('.css'));

    if (!mainJs) {
        throw new Error('build: main.js output not found in metafile');
    }

    const head = [];
    if (css) {
        head.push(`<link rel="stylesheet" href="${css}">`);
    }

    const body = [];
    body.push(`<script type="module" src="${mainJs}"></script>`);

    if (production) {
        body.push(`<script type="module">
if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('sw.js', { scope: '/' }).catch(() => {});
}
</script>`);
    }

    html = html.replace('<!-- @app-head -->', head.join('\n        '));
    html = html.replace('<!-- @app-body -->', body.join('\n    '));

    writeFileSync(join(outDir, 'index.html'), html);
}

/**
 * Emit sw.js with the generated precache manifest injected as a constant
 * (decision-pwa-custom-sw.md §4). Stable, unhashed filename.
 */
function processServiceWorker(names) {
    const hashed = names.filter((n) => n.endsWith('.js') || n.endsWith('.css'));

    const precache = [
        '/index.html',
        '/manifest.webmanifest',
        '/sw.js',
        '/favicon.ico',
        ...hashed.map((n) => '/' + n),
        ...listStaticAssets().map((p) => '/' + p),
    ];

    let sw = readFileSync(join(srcDir, 'sw.js'), 'utf8');
    sw = sw.replace(
        '/* @precache-manifest */',
        'const PRECACHE_MANIFEST = ' + JSON.stringify(precache, null, 4) + ';',
    );

    writeFileSync(join(outDir, 'sw.js'), sw);
}

function listStaticAssets() {
    const files = [];
    const walk = (dir) => {
        for (const entry of readdirSync(join(outDir, dir), { withFileTypes: true })) {
            const p = join(dir, entry.name);
            if (entry.isDirectory()) walk(p);
            else files.push(p.split('\\').join('/'));
        }
    };
    for (const dir of staticDirs) {
        if (existsSync(join(outDir, dir))) walk(dir);
    }
    return files.sort();
}

function postProcess(result) {
    const names = listOutputs(result.metafile);
    processIndexHtml(names);
    processServiceWorker(names);
}

const buildOptions = {
    bundle: true,
    define: {
        __RDIO_VERSION__: JSON.stringify(packageInfo.version),
    },
    entryNames: production ? '[name].[hash]' : '[name]',
    entryPoints: [join(srcDir, 'main.js')],
    external: ['/assets/*'],
    format: 'esm',
    legalComments: 'none',
    loader: {
        '.svg': 'text',
    },
    metafile: true,
    minify: production,
    outdir: outDir,
    sourcemap: !production,
    splitting: true,
    target: ['es2022'],
    write: true,
};

// The build runs when this file is run. A test imports resolveOutDir from it, and importing it must not
// rebuild the webapp.
const runThisBuild = process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url);

if (runThisBuild) {
    cleanOutdir();

    if (watch) {
        const context = await esbuild.context(buildOptions);
        copyStatic();
        postProcess(await context.rebuild());
        console.log('[client-ng] watching for changes…');
        context.watch();
    } else {
        copyStatic();
        const result = await esbuild.build(buildOptions);
        postProcess(result);

        const sizes = Object.entries(result.metafile.outputs)
            .map(([p, o]) => `  ${outName(p)}  ${(o.bytes / 1024).toFixed(1)} KB`)
            .join('\n');
        console.log(`[client-ng] built v${packageInfo.version} into ${outDir}\n${sizes}`);
    }
}
