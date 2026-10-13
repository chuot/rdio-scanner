import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

// The stylesheets the client build feeds to esbuild. A `//` is not a comment in CSS: the browser
// throws away the text from the `//` to the next semicolon, and the declaration it throws away is
// the one the line was standing next to. A stylesheet that carries one cannot be allowed to reach a
// bundle, so the whole set is checked here.
//
// The scan walks the text the way the browser reads it: inside `/* */` a `//` is comment text and
// harmless, inside a quoted URL (a font or an icon path) it is part of the string, and anywhere else
// it is a declaration the browser discards.
//
// The paths are read from the presentation directory, two directories above the tests' own
// application directory.
const presentationDir = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..');

const STYLESHEETS = [
    join(presentationDir, 'common', 'frontend', 'styles', 'theme.css'),
    join(presentationDir, 'admin', 'frontend', 'styles', 'admin.css'),
    join(presentationDir, 'scanner', 'frontend', 'styles', 'rdio.css'),
];

function foreignLineComments(css) {
    const found = [];

    // The scan tracks the line the construct started on, the way an operator reads a reported line.
    const lineAt = (offset) => 1 + (offset === 0 ? 0 : css.slice(0, offset).split('\n').length - 1);

    let i = 0;

    while (i < css.length) {
        const open = css.indexOf('/*', i);
        const slash = css.indexOf('//', i);
        const double = css.indexOf('"', i);
        const single = css.indexOf("'", i);

        const next = [open, slash, double, single].filter((at) => at >= 0).sort((a, b) => a - b)[0];

        if (next === undefined) break;

        if (next === open) {
            const close = css.indexOf('*/', open + 2);

            // An unterminated block comment runs to the end of the stylesheet, as the browser reads it.
            i = close < 0 ? css.length : close + 2;

            continue;
        }

        if (next === double || next === single) {
            const quoteAt = Math.min(...[double, single].filter((at) => at >= 0));
            const closer = css.indexOf(css[quoteAt], quoteAt + 1);

            i = closer < 0 ? css.length : closer + 1;

            continue;
        }

        found.push(lineAt(slash));

        i = slash + 2;
    }

    return found;
}

describe('stylesheets — a comment the browser does not have', () => {
    it('reads the three stylesheets the client build consumes', () => {
        for (const path of STYLESHEETS) {
            assert.ok(readFileSync(path, 'utf8').length > 0, `${path} is empty`);
        }
    });

    it('carries no line comment outside a quoted URL and outside a block comment', () => {
        for (const path of STYLESHEETS) {
            const lines = foreignLineComments(readFileSync(path, 'utf8'));

            assert.deepEqual(lines, [], `${path} carries a // line comment on line(s) ${lines.join(', ')}; CSS has no line comment and the browser discards the declaration it sits on`);
        }
    });

    it('names the line comment inside a block comment as harmless', () => {
        assert.deepEqual(foreignLineComments('a { /* a // b */ margin: 0 auto }'), []);
    });

    it('names the line comment inside a quoted URL as harmless', () => {
        assert.deepEqual(foreignLineComments('a { background: url("https://example.test/f.png"); margin: 0 auto }'), []);
    });

    it('names a line comment that swallows the declaration it sits on', () => {
        assert.deepEqual(foreignLineComments('a { // room\n margin: 0 auto }'), [1]);
    });
});
