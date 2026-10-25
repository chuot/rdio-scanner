import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

// reduced motion, proven in the stylesheets that run it
// every animation and transition is guarded on its own selector
// see the reduced-motion-scan maintainer note.
const presentationDir = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..');

const STYLESHEETS = {
    theme: join(presentationDir, 'common', 'frontend', 'styles', 'theme.css'),
    admin: join(presentationDir, 'admin', 'frontend', 'styles', 'admin.css'),
    scanner: join(presentationDir, 'scanner', 'frontend', 'styles', 'rdio.css'),
};

// The words a shorthand animation value carries that are not the animation's name.
const ANIMATION_WORDS = new Set([
    'none',
    'infinite',
    'linear',
    'ease',
    'ease-in',
    'ease-out',
    'ease-in-out',
    'step-start',
    'step-end',
    'forwards',
    'backwards',
    'both',
    'normal',
    'reverse',
    'alternate',
    'alternate-reverse',
    'running',
    'paused',
    'initial',
    'inherit',
    'unset',
    'revert',
]);

function blankComments(css) {
    return css.replace(/\/\*[\s\S]*?\*\//g, (comment) => comment.replace(/[^\n]/g, ' '));
}

function lineAt(text, index) {
    return 1 + text.slice(0, index).split('\n').length - 1;
}

// top-level blocks: selector, inner text, opening line; nesting recursed
function blocks(css) {
    const stripped = blankComments(css);
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

        found.push({
            selector: stripped.slice(at, open).trim(),
            body: stripped.slice(open + 1, close - 1),
            line: lineAt(stripped, open),
        });

        at = close;
    }

    return found;
}

// a rule's own declarations: nested blocks removed, split on semicolons
function declarations(body) {
    let text = '';
    let depth = 0;

    for (const char of body) {
        if (char === '{') depth += 1;
        else if (char === '}') depth -= 1;
        else if (depth === 0) text += char;
    }

    return text.split(';').map((decl) => decl.trim()).filter(Boolean);
}

function selectors(selector) {
    return selector.split(',').map((part) => part.trim()).filter(Boolean);
}

function animationNames(decls) {
    const names = [];

    for (const decl of decls) {
        const rule = /^(animation(?:-name)?)\s*:\s*(.+)$/.exec(decl);

        if (!rule) continue;

        const value = rule[2].trim();

        if (rule[1] === 'animation' && /\bnone\b/.test(value)) continue;

        for (const token of value.split(/[\s,]+/)) {
            if (!token || ANIMATION_WORDS.has(token)) continue;
            if (/^[\d.]/.test(token)) continue;
            if (/^(cubic-bezier|steps|var|local|url)\(/.test(token)) continue;

            names.push(token);
        }
    }

    return names;
}

function setsNone(decls, property) {
    return decls.some((decl) => new RegExp(`^${property}\\s*:\\s*(none|0)\\b`).test(decl));
}

function declares(decls, property) {
    return decls.filter((decl) => new RegExp(`^${property}\\b.*\\s*:`).test(decl));
}

// The rules a page runs, and the rules the preference runs in its place.
function cascade(name) {
    const css = readFileSync(STYLESHEETS[name], 'utf8');

    const running = [];
    const guards = [];

    for (const block of blocks(css)) {
        const media = block.selector.startsWith('@media');

        if (media) {
            // only the reduce block counts: width media blocks apply in every motion state
            if (!/prefers-reduced-motion/.test(block.selector) || !/reduce/.test(block.selector)) {
                for (const inner of blocks(block.body)) running.push({ ...inner, file: name });
            }

            continue;
        }

        if (block.selector.startsWith('@')) continue;

        running.push({ ...block, file: name });
    }

    for (const block of blocks(css)) {
        if (!block.selector.startsWith('@media')) continue;
        if (!/prefers-reduced-motion/.test(block.selector) || !/reduce/.test(block.selector)) continue;

        for (const inner of blocks(block.body)) {
            guards.push({
                ...inner,
                file: name,
                stopsAnimation: setsNone(declarations(inner.body), 'animation(-name)?'),
                stopsTransition: setsNone(declarations(inner.body), 'transition(-property|-duration)?'),
                freezes: declarations(inner.body).some((decl) => /^animation-play-state\s*:/.test(decl)),
            });
        }
    }

    return { running, guards };
}

function unguarded(name) {
    const { running, guards } = cascade(name);

    const missing = [];

    for (const rule of running) {
        const parts = selectors(rule.selector);

        const animations = animationNames(declarations(rule.body));

        if (animations.length) {
            const covered = guards.filter((guard) => guard.stopsAnimation && selectors(guard.selector).some((part) => parts.includes(part)));
            const stopped = new Set(animations.filter((animation) => covered.length));

            for (const animation of animations) {
                if (stopped.has(animation)) continue;

                missing.push(`${rule.file} ${rule.line}: ${rule.selector} { animation: ${animation} }`);
            }
        }

        if (declares(declarations(rule.body), 'transition').length) {
            const covered = guards.some((guard) => guard.stopsTransition && selectors(guard.selector).some((part) => parts.includes(part)));

            if (!covered) missing.push(`${rule.file} ${rule.line}: ${rule.selector} { transition }`);
        }
    }

    return missing;
}

describe('reduced motion — the cascade the preference gets', () => {
    it('guards every animation the stylesheets run, on the selector that runs it', () => {
        const missing = ['theme', 'admin', 'scanner'].flatMap(unguarded);

        assert.deepEqual(missing, [], `no prefers-reduced-motion rule stops: ${missing.join(' | ')}`);
    });

    it('stops an animation by taking it away, not by freezing it mid-cycle', () => {
        const frozen = ['theme', 'admin', 'scanner']
            .flatMap((name) => cascade(name).guards)
            .filter((guard) => guard.freezes)
            .map((guard) => `${guard.file} ${guard.line}: ${guard.selector}`);

        assert.deepEqual(frozen, [], `a reduced-motion rule freezes mid-cycle: ${frozen.join(' | ')}`);
    });

    it('names the preference in every stylesheet the applications ship', () => {
        const silent = ['theme', 'admin', 'scanner'].filter((name) => cascade(name).guards.length === 0);

        assert.deepEqual(silent, [], `no prefers-reduced-motion block in: ${silent.join(' | ')}`);
    });
});
