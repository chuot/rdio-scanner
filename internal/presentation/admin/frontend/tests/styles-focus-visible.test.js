// tests for focus visibility: the cascade the keyboard gets
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

// the focus indicator, proven in the stylesheets that draw it
// a field-box ring may replace a control ring, but only paired with it
// see the focus-visible-readings maintainer note.
const presentationDir = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..');

const STYLESHEETS = {
    theme: join(presentationDir, 'common', 'frontend', 'styles', 'theme.css'),
    admin: join(presentationDir, 'admin', 'frontend', 'styles', 'admin.css'),
    scanner: join(presentationDir, 'scanner', 'frontend', 'styles', 'rdio.css'),
};

// tags the keyboard can stop on: suppressions here lose the operator's place
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

// a press is not a focus: outline rules in these states do not touch the keyboard
// a disabled control takes no focus at all
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

// the last selector part is the element the declarations land on
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

// a field control's ring moves to its box: fill, radii, underline frame the box
// without that box ring the control's suppression is still reported
function fieldControlSuppression(part) {
    const last = subject(part);

    return /(^|[,>+~])(select|input)[\s:.,[\w-]*$/.test(last) && part.includes('rdio-form-field');
}

function boxDrawnRings(name) {
    return rules(readFileSync(STYLESHEETS[name], 'utf8')).filter((rule) =>
        /\.rdio-field-box:has\(:focus-visible\)/.test(rule.selector) &&
        /outline\s*:\s*(?!none\b|0\b)/.test(rule.body));
}

function suppressions(name) {
    const found = [];
    const replacedBy = boxDrawnRings(name).length > 0;

    for (const rule of rules(readFileSync(STYLESHEETS[name], 'utf8'))) {
        const suppressed = outlineSuppression(rule.body);

        if (!suppressed.length) continue;

        for (const part of selectors(rule.selector)) {
            // rules scoped to a press, hover, or disabled state are not keyboard focus rules
            if (PRESS_STATES.some((state) => subject(part).includes(state))) continue;
            if (!focusableSubject(part)) continue;
            if (fieldControlSuppression(part) && replacedBy) continue;

            found.push(`${name} ${rule.line}: ${part} { ${suppressed.join('; ')} }`);
        }
    }

    return found;
}

function focusVisibleRules(name) {
    return rules(readFileSync(STYLESHEETS[name], 'utf8'))
        .filter((rule) => selectors(rule.selector).some((part) => part.includes(':focus-visible')));
}

// an entry: the control family, its stylesheet, and the proving selector shape
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
    { family: "the listener's field boxes", sheet: 'scanner', part: (p) => p.includes('rdio-field-box') && p.includes(':has(:focus-visible)') },
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
        // every ring is the accent; light fields use grey ink, forced mode uses Highlight
        // see the focus-ring-ink maintainer note.
        const wrong = [];

        for (const sheet of ['theme', 'admin', 'scanner']) {
            const inForcedMode = new Set(forcedColorRules(sheet).map((rule) => `${rule.selector}|${rule.body}`));

            for (const rule of focusVisibleRules(sheet)) {
                const outline = (rule.body.match(/outline\s*:\s*([^;]+)/) ?? [])[1];

                if (!outline) continue;

                // an outline suppression is not a ring: the suppression check owns it
                if (/^\s*(none|0)\b/.test(outline)) continue;

                const allowed = inForcedMode.has(`${rule.selector}|${rule.body}`) ? /Highlight/ : /--rdio-accent|#424242/;

                if (!allowed.test(outline)) wrong.push(`${sheet} ${rule.line}: outline: ${outline.trim()}`);

                const offset = (rule.body.match(/outline-offset\s*:\s*([^;]+)/) ?? [])[1];

                if (!offset || parseFloat(offset) < 2) wrong.push(`${sheet} ${rule.line}: outline-offset: ${(offset ?? 'none').trim()}`);
            }
        }

        assert.deepEqual(wrong, [], `the indicator is not the accent outline at a 2px offset: ${wrong.join(' | ')}`);
    });
});

// under a forced palette the system owns the colours; the shapes stay
// every sheet needs a forced-colors block, and no forced-color-adjust: none
// see the forced-colours maintainer note.

function blocksAtOneLevel(css) {
    const stripped = css.replace(/\/\*[\s\S]*?\*\//g, (comment) => comment.replace(/[^\n]/g, ' '));

    const found = [];

    let at = 0;

    while (at < stripped.length) {
        const open = stripped.indexOf('{', at);

        if (open === -1) break;

        let depth = 1;
        let close = open + 1;

        while (close < stripped.length && depth > 0) {
            if (stripped[close] === '{') depth += 1;
            else if (stripped[close] === '}') depth -= 1;

            close += 1;
        }

        found.push({ selector: stripped.slice(at, open).trim(), body: stripped.slice(open + 1, close - 1) });

        at = close;
    }

    return found;
}

function declarationsOnly(body) {
    let text = '';
    let depth = 0;

    for (const char of body) {
        if (char === '{') depth += 1;
        else if (char === '}') depth -= 1;
        else if (depth === 0) text += char;
    }

    return text.split(';').map((decl) => decl.trim()).filter(Boolean);
}

function forcedColorRules(name) {
    const css = readFileSync(STYLESHEETS[name], 'utf8');

    return blocksAtOneLevel(css)
        .filter((block) => block.selector.startsWith('@media') && /forced-colors\s*:\s*active/.test(block.selector))
        .flatMap((block) => blocksAtOneLevel(block.body));
}

function everyDeclaration(name) {
    const walk = (blocks) => blocks.flatMap((block) => [...declarationsOnly(block.body), ...walk(blocksAtOneLevel(block.body))]);

    return walk(blocksAtOneLevel(readFileSync(STYLESHEETS[name], 'utf8')));
}

describe('forced colours — the cascade the system palette gets', () => {
    it('carries a forced-colors: active block in every stylesheet the applications ship', () => {
        const silent = ['theme', 'admin', 'scanner'].filter((name) => forcedColorRules(name).length === 0);

        assert.deepEqual(silent, [], `no forced-colors: active block in: ${silent.join(' | ')}`);
    });

    it('never opts out of the mode the operator asked the system for', () => {
        const opted = ['theme', 'admin', 'scanner'].flatMap((name) =>
            everyDeclaration(name)
                .filter((decl) => /^forced-color-adjust\s*:\s*none\b/.test(decl))
                .map((decl) => `${name}: ${decl}`),
        );

        assert.deepEqual(opted, [], `forced-color-adjust: none in: ${opted.join(' | ')}`);
    });
});
