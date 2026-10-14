import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

// The focus indicator, proven in the stylesheets that draw it.
//
// A keyboard operator sees where they are standing by seeing what the page draws around the control
// they are on. Two things can take that away: a rule that suppresses the outline on a control the
// keyboard can reach, and a family of controls no rule covers. Both are read here out of the
// cascade the browser is given, because both are the cascade's doing; the harness walks the real
// route and measures the drawn ring against what sits beside it.
//
// The paths are read from the presentation directory, three directories above this tests' own
// application directory — the same shape `styles-admin-css.test.js` reads.
const presentationDir = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..');

const STYLESHEETS = {
    theme: join(presentationDir, 'common', 'frontend', 'styles', 'theme.css'),
    admin: join(presentationDir, 'admin', 'frontend', 'styles', 'admin.css'),
    scanner: join(presentationDir, 'scanner', 'frontend', 'styles', 'rdio.css'),
};

// The controls a keyboard can stop on, by the names the applications give them. A suppression that
// lands on one of these is the operator losing their place; a suppression that lands on a caption
// or a card is nothing of the kind.
const FOCUSABLE_TAGS = ['a', 'button', 'input', 'select', 'textarea', 'summary'];
const FOCUSABLE_NAMES = [
    'rdio-admin-nav',
    'rdio-admin-section-bar',
    'rdio-admin-row',
    'rdio-admin-todos-banner',
    'rdio-admin-login',
    'rdio-button',
    'rdio-button-mini',
    'rdio-button-plain',
    'rdio-form-field',
    'rdio-select',
    'rdio-toggle',
    'rdio-paginator',
    'rdio-dialog',
    'rdio-controls',
    'rdio-search-panel',
    'rdio-select-panel',
    'panel-close',
    'row-action',
    'drag-handle',
    'todo-step',
];

// The states a suppression is allowed to live in. A press is not a focus: the beveled listener
// buttons flatten under the pointer, and the rule that does it takes the native ring off the press,
// not off the keyboard. A disabled control takes no focus at all.
const PRESS_STATES = [':active', ':disabled', ':enabled', ':hover'];

function rules(css) {
    const stripped = css.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, ' '));

    const found = [];
    const at = /([^{}]+)\{([^{}]*)\}/g;

    let match;

    while ((match = at.exec(stripped)) !== null) {
        const selector = match[1].trim();

        // A keyframes step and an at-rule are not a rule with a subject the keyboard can land on.
        if (selector.startsWith('@') || /^\d+%$/.test(selector)) continue;

        found.push({
            selector,
            body: match[2],
            line: 1 + stripped.slice(0, match.index).split('\n').length - 1,
        });
    }

    return found;
}

function selectors(selector) {
    return selector.split(',').map((part) => part.trim()).filter(Boolean);
}

// The compound the rule paints: the last part of the selector is the element the declarations land
// on, which is the element the keyboard is standing on.
function subject(part) {
    const compounds = part.split(/\s+/);

    return compounds[compounds.length - 1] ?? '';
}

function focusableSubject(part) {
    const last = subject(part);

    if (FOCUSABLE_TAGS.some((tag) => new RegExp(`(^|[,>+~])${tag}[\\s:.,[\\w-]*$`).test(last))) return true;
    if (/\[tabindex/.test(last)) return true;

    return FOCUSABLE_NAMES.some((name) => part.includes(name));
}

function outlineSuppression(body) {
    const declarations = body.split(';').map((d) => d.trim()).filter(Boolean);

    return declarations.filter((d) => /^outline(-style)?\s*:\s*(none|0)\b/.test(d));
}

function suppressions(name) {
    const found = [];

    for (const rule of rules(readFileSync(STYLESHEETS[name], 'utf8'))) {
        const suppressed = outlineSuppression(rule.body);

        if (!suppressed.length) continue;

        for (const part of selectors(rule.selector)) {
            // A rule scoped to a press, a hover, or a disabled control is not the rule the operator
            // sees while they hold the keyboard on a control.
            if (PRESS_STATES.some((state) => subject(part).includes(state))) continue;
            if (!focusableSubject(part)) continue;

            found.push(`${name} ${rule.line}: ${part} { ${suppressed.join('; ')} }`);
        }
    }

    return found;
}

function focusVisibleRules(name) {
    return rules(readFileSync(STYLESHEETS[name], 'utf8'))
        .filter((rule) => selectors(rule.selector).some((part) => part.includes(':focus-visible')));
}

// The families the requirement names, each with the stylesheet that must draw its indicator and the
// shape of the selector that proves the family is the one being painted.
const FAMILIES = [
    { family: 'navigation buttons', sheet: 'admin', part: (p) => p.includes('rdio-admin-nav') && p.includes('button') },
    { family: 'section bar buttons', sheet: 'admin', part: (p) => p.includes('rdio-admin-section-bar') },
    { family: 'row summaries', sheet: 'admin', part: (p) => p.includes('summary') },
    { family: 'row action buttons', sheet: 'admin', part: (p) => p.includes('rdio-admin-row') && p.includes('button') },
    { family: 'links the operator follows', sheet: 'admin', part: (p) => p.includes('todo-step') },
    { family: 'form controls', sheet: 'theme', part: (p) => /(^|[\s>+~])(input|textarea|select|rdio-form-field|rdio-select)\b[\s:.,[\w-]*$/.test(p) },
    { family: 'toggles', sheet: 'theme', part: (p) => p.includes('rdio-toggle') },
    { family: 'paginators', sheet: 'theme', part: (p) => p.includes('rdio-paginator') },
    { family: 'dialog buttons', sheet: 'theme', part: (p) => p.includes('rdio-dialog') },
    { family: 'links the shared primitives draw', sheet: 'theme', part: (p) => /(^|[\s>+~])a\b/.test(p) },
    { family: "the listener's control buttons", sheet: 'scanner', part: (p) => p.includes('rdio-controls') && p.includes('rdio-button') },
];

describe('focus indicators — the cascade the keyboard gets', () => {
    it('suppresses the outline on nothing the keyboard can stop on', () => {
        const found = ['theme', 'admin', 'scanner'].flatMap(suppressions);

        assert.deepEqual(found, [], `the cascade takes the focus outline off a focusable control: ${found.join(' | ')}`);
    });

    it('draws the indicator on every family the requirement names', () => {
        const missing = [];

        for (const { family, sheet, part } of FAMILIES) {
            const covered = focusVisibleRules(sheet).some((rule) => selectors(rule.selector).some(part));

            if (!covered) missing.push(`${family} (${sheet})`);
        }

        assert.deepEqual(missing, [], `no :focus-visible rule covers: ${missing.join(' | ')}`);
    });

    it('draws the indicator as the accent outline the pages already use, at the offset that clears the control', () => {
        // One ring everywhere, so a keyboard operator reads the same shape on every control. The
        // listener's light material fields are the one place the ring is not the accent: on the
        // light fill those fields sit on, the accent green measures 2.6:1 against the fill, under
        // the 3:1 floor an indicator has to clear, and those fields' own ink — the grey their text
        // and their pressed line already use — measures 9.4:1. The shape and the distance are the
        // same there; only the ink differs.
        const wrong = [];

        for (const sheet of ['theme', 'admin', 'scanner']) {
            for (const rule of focusVisibleRules(sheet)) {
                const outline = (rule.body.match(/outline\s*:\s*([^;]+)/) ?? [])[1];

                if (!outline) continue;

                if (!/--rdio-accent|#424242/.test(outline)) wrong.push(`${sheet} ${rule.line}: outline: ${outline.trim()}`);

                const offset = (rule.body.match(/outline-offset\s*:\s*([^;]+)/) ?? [])[1];

                if (!offset || parseFloat(offset) < 2) wrong.push(`${sheet} ${rule.line}: outline-offset: ${(offset ?? 'none').trim()}`);
            }
        }

        assert.deepEqual(wrong, [], `the indicator is not the accent outline at a 2px offset: ${wrong.join(' | ')}`);
    });
});
