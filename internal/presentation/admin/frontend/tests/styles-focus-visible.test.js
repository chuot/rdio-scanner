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
// The light material fields are the one place the ring is not drawn on the control the keyboard is
// standing on: the field's visible surface is its box, the control sits inset inside it, and the
// cascade frames the box and takes the browser's native ring off the control. That pairing —
// suppression on the control beside a drawn ring on the field's own box — is what the readings
// below allow, and only together.
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

// A field control's suppression is the field's own arrangement when the field's box draws the
// ring in its place: the cascade takes the browser's native ring off the control and frames the
// whole field — fill, corner radii, underline — on the box that holds the control. A suppression
// with no box-level ring in the same stylesheet is still the operator losing their place, and it
// is reported.
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
            // A rule scoped to a press, a hover, or a disabled control is not the rule the operator
            // sees while they hold the keyboard on a control.
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
        // One ring everywhere, so a keyboard operator reads the same shape on every control. The
        // listener's light material fields are the one place the ring is not the accent: on the
        // light fill those fields sit on, the accent green measures 2.6:1 against the fill, under
        // the 3:1 floor an indicator has to clear, and those fields' own ink — the grey their text
        // and their pressed line already use — measures 9.4:1. The shape and the distance are the
        // same there; only the ink differs.
        //
        // Under a forced palette the ring is drawn in the colour the system uses to say "here",
        // `Highlight`, at the same shape and the same distance: the accent is a colour the operator
        // asked the system to replace. A rule inside the mode's own block is allowed the system's
        // colour, and only there.
        const wrong = [];

        for (const sheet of ['theme', 'admin', 'scanner']) {
            const inForcedMode = new Set(forcedColorRules(sheet).map((rule) => `${rule.selector}|${rule.body}`));

            for (const rule of focusVisibleRules(sheet)) {
                const outline = (rule.body.match(/outline\s*:\s*([^;]+)/) ?? [])[1];

                if (!outline) continue;

                // A suppression is not a drawn ring; the suppression reading owns it and only
                // allows it where a box-level ring replaces it.
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

// --- forced colours ---------------------------------------------------------------
//
// Under a forced palette the system owns the colours: the page's greys, its accents, and its fills
// are replaced by the colours the operator asked the system for. What the page has to keep is the
// shape of its controls — the border that says a card is a card, the line that says a field is a
// field, the ring that says where the keyboard is standing — drawn in the system's own colours. A
// stylesheet with no block for the mode is a stylesheet whose shapes are painted in the colours the
// system was asked to replace, and a stylesheet that sets `forced-color-adjust: none` is the same
// stylesheet opting out of the mode entirely.
//
// The blocks are read the way the browser reads them: comments blanked, a media block's own rules
// read from inside it.

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
