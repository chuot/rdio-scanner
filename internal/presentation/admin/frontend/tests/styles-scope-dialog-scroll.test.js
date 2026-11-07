// tests for the scope dialog: the tree scrolls inside a box that cannot
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

// the scope dialog's tree scrolls itself: the box has no height limit
// scrolling the tree keeps the title and the actions in view while rows
// move.
const presentationDir = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..');

const STYLESHEETS = {
    admin: join(presentationDir, 'admin', 'frontend', 'styles', 'admin.css'),
    theme: join(presentationDir, 'common', 'frontend', 'styles', 'theme.css'),
    scanner: join(presentationDir, 'scanner', 'frontend', 'styles', 'rdio.css'),
};

// the selection component's painted root: this element scrolls when rows overflow
const TREE = 'rdio-systems-selection .rdio-systems-selection';

// the dialog box: no height limit, so the tree's scroll is the only valve
const BOX = '.rdio-dialog-box';

function rules(css) {
    const stripped = css.replace(/\/\*[\s\S]*?\*\//g, (comment) => comment.replace(/[^\n]/g, ' '));

    const found = [];
    const at = /([^{}]+)\{([^{}]*)\}/g;

    let match;

    while ((match = at.exec(stripped))) {
        const selector = match[1].trim().replace(/\s+/g, ' ');

        const declarations = new Map();

        for (const part of match[2].split(';')) {
            const separator = part.indexOf(':');

            if (separator < 0) continue;

            const name = part.slice(0, separator).trim();
            const value = part.slice(separator + 1).trim();

            if (name) declarations.set(name, value);
        }

        found.push({ selector, declarations });
    }

    return found;
}

function ruleFor(css, selector) {
    return rules(css).find((rule) => rule.selector === selector) ?? null;
}

describe('the administration stylesheet — the scope dialog tree scrolls inside a box that cannot', () => {
    it('carries a scroll rule on the tree it is there to scroll', () => {
        const rule = ruleFor(readFileSync(STYLESHEETS.admin, 'utf8'), TREE);

        assert.ok(rule, `the administration stylesheet carries no rule on \`${TREE}\``);

        assert.equal(rule.declarations.get('overflow-y'), 'auto', 'the tree cannot scroll when its rows outgrow the viewport');

        const maxHeight = rule.declarations.get('max-height') ?? '';

        assert.match(maxHeight, /^\d+(?:\.\d+)?vh$/, `the tree's height limit is not taken from the viewport: ${maxHeight || 'none'}`);
    });

    it('leaves the limit on the tree, not on the dialog box the tree is opened in', () => {
        const box = ruleFor(readFileSync(STYLESHEETS.theme, 'utf8'), BOX);

        assert.ok(box, 'the shared dialog box carries no rule to read');

        assert.equal(box.declarations.has('max-height'), false, 'the dialog box limits its own height, so the tree is left out of view');
        assert.equal(box.declarations.has('overflow'), false, 'the dialog box scrolls as a whole, carrying the title and the actions away with the rows');
        assert.equal(box.declarations.has('overflow-y'), false, 'the dialog box scrolls as a whole, carrying the title and the actions away with the rows');
    });

    it('keeps the valve in the administration application, where the scope dialog lives', () => {
        for (const name of ['theme', 'scanner']) {
            assert.equal(ruleFor(readFileSync(STYLESHEETS[name], 'utf8'), TREE), null, `${name} carries the administration tree's scroll rule, which it has no tree to apply it to`);
        }
    });
});
