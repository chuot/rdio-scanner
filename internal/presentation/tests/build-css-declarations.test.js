import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import esbuild from 'esbuild';

// The banner rule of the administration stylesheet is a declaration the browser has to receive:
// `margin: 0 auto var(--rdio-space-5)` is what centres the todos banner and keeps the 24 px step
// above the section card. A `//` line comment in front of it makes the browser throw the declaration
// away, and the bundle looks built while the page is unstyled.
//
// So the stylesheet goes through the build's own minifier — the same esbuild, the same production
// settings — and the text that reaches the bundle is what gets asserted.

const presentationDir = join(dirname(fileURLToPath(import.meta.url)), '..');

const adminStylesheet = join(presentationDir, 'admin', 'frontend', 'styles', 'admin.css');

async function builtStylesheet() {
    const result = await esbuild.build({
        bundle: true,
        entryPoints: [adminStylesheet],
        external: ['/assets/*', 'assets/*'],
        minify: true,
        outfile: join('/tmp/opencode', 'admin-built.css'),
        write: false,
    });

    // esbuild hands the bytes back as a Uint8Array; spreading it into String() turns the whole
    // stylesheet into a list of numbers.
    return new TextDecoder('utf8').decode(result.outputFiles[0].contents);
}

function bannerRule(css) {
    const at = css.indexOf('.rdio-admin-todos-banner{');

    assert.ok(at >= 0, `the built stylesheet carries no todos banner rule; it carries ${JSON.stringify(css.slice(0, 200))}`);

    return css.slice(at, css.indexOf('}', at));
}

describe('build — the administration stylesheet as the build emits it', () => {
    it('carries the banner centring and spacing declaration the browser needs', async () => {
        const rule = bannerRule(await builtStylesheet());

        assert.ok(
            rule.includes('margin:0 auto var(--rdio-space-5)'),
            `the built banner rule is ${JSON.stringify(rule)}; the declaration that centres the banner and keeps the room above the section card did not survive the build`,
        );
    });

    it('carries the width cap that lines the banner up with the section card', async () => {
        const rule = bannerRule(await builtStylesheet());

        assert.ok(
            rule.includes('max-width:calc(var(--rdio-content-width) + (2 * var(--rdio-card-padding)))'),
            `the built banner rule is ${JSON.stringify(rule)}; the banner is not capped to the section card's box`,
        );

        assert.ok(rule.includes('box-sizing:border-box'), `the built banner rule is ${JSON.stringify(rule)}; the cap is not measured from border to border`);
    });

    it('carries no line comment the browser would throw away', async () => {
        const css = await builtStylesheet();

        const outside = css.replace(/"[^"]*"/g, '""').replace(/'[^']*'/g, "''");

        assert.ok(!outside.includes('//'), 'the built stylesheet carries a // outside a quoted URL; the browser discards the text that follows it');
    });

    it('carries the logs body row height that levels the filler rows with the filled rows', async () => {
        const css = await builtStylesheet();

        const at = css.indexOf('.rdio-admin-card.logs tbody tr{');

        assert.ok(at >= 0, 'the built stylesheet carries no logs body row rule; the filler rows of a short logs page have nothing to hold them at the filled rows\' height');

        const rule = css.slice(at, css.indexOf('}', at));

        assert.ok(
            rule.includes('height:44px'),
            `the built logs body row rule is ${JSON.stringify(rule)}; the declaration that holds the filler rows at the filled rows' 44 px height did not survive the build`,
        );
    });

    // The dashboard's two sizes the browser has to receive: the grid's 280 px track decides how many cards
    // fit across the content column and when they drop to one line each, and the bar's floor is what keeps
    // an hour with nothing in it a stub on the page instead of a gap an operator has to interpret.
    it('carries the dashboard card grid and the floor under an empty chart bar', async () => {
        const css = await builtStylesheet();

        const gridAt = css.indexOf('.rdio-dashboard-cards{');

        assert.ok(gridAt >= 0, 'the built stylesheet carries no dashboard card grid rule; the cards have no width to lay themselves out in');

        const grid = css.slice(gridAt, css.indexOf('}', gridAt));

        assert.ok(
            grid.includes('repeat(auto-fit,minmax(280px,1fr))'),
            `the built dashboard grid rule is ${JSON.stringify(grid)}; the track that decides how many cards fit did not survive the build`,
        );

        const barAt = css.indexOf('.rdio-dashboard-bar{');

        assert.ok(barAt >= 0, 'the built stylesheet carries no dashboard chart bar rule; the 24 hours have no bar to draw');

        const bar = css.slice(barAt, css.indexOf('}', barAt));

        assert.ok(
            bar.includes('min-width:var(--rdio-space-1)'),
            `the built dashboard bar rule is ${JSON.stringify(bar)}; the floor that keeps an empty hour visible did not survive the build`,
        );

        assert.ok(
            bar.includes('background:var(--rdio-accent)'),
            `the built dashboard bar rule is ${JSON.stringify(bar)}; the bar has no face of its own`,
        );
    });
});
