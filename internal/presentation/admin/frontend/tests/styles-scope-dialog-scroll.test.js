import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

// The scope dialog's tree scrolls, proven in the stylesheet that scrolls it.
//
// The dialog that scopes an access code, an API key, or a downstream holds a tree of the deployment's
// groups, tags, systems, and talkgroups inside a box that has no height limit and no overflow of its own
// (`.rdio-dialog-box` in `theme.css`). The only thing standing between a deployment with dozens of
// talkgroups and a row nobody can reach is the rule that scrolls the tree inside that box — and a named
// row is a longer row than a numbered one, with a row that wraps being taller still. Nothing tested that
// rule, which is what made it droppable: an edit could take it out with every suite green and the row
// unreachable on a phone.
//
// The readings are the cascade the browser is given. The rule is found by the selector it is written on
// and read there, both declarations; the box it scrolls is read on its own selector, because the valve
// being on the tree rather than on the box is what keeps the dialog's title and its Cancel and Select
// actions in view while the rows move. The rule is carried by the administration stylesheet alone: the
// listener application's dialogs are not this rule's to change.
//
// The paths are read from the presentation directory, three directories above this tests' own application
// directory — the same shape `styles-admin-css.test.js` and `styles-focus-visible.test.js` read.
const presentationDir = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..');

const STYLESHEETS = {
    admin: join(presentationDir, 'admin', 'frontend', 'styles', 'admin.css'),
    theme: join(presentationDir, 'common', 'frontend', 'styles', 'theme.css'),
    scanner: join(presentationDir, 'scanner', 'frontend', 'styles', 'rdio.css'),
};

// The tree's own root: the selection component paints it inside its host element, and it is the element
// that moves when the deployment outgrows the viewport.
const TREE = 'rdio-systems-selection .rdio-systems-selection';

// The box the tree is opened in. It carries no height limit, which is why the tree's own scroll is the
// only valve the dialog has.
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
