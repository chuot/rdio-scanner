// The hourglass the flag row draws is not in any font the listener ships.
//
// The listener's own font rules carry `unicode-range` lists — latin and latin-ext — and the
// hourglass the timed-avoid flag draws sits outside every one of them, so the glyph can only ever
// come from a font the browser reaches for on its own. That is not a small detail on this row: the
// fallback Linux takes by default does not carry the glyph at all, and neither does the older Noto
// Sans Symbols, so left to chance the flag can render an empty box where the operator reads a window
// on the feed.
//
// The cascade's answer is a family offered for that one codepoint. The `unicode-range` is what keeps
// it surgical: the family is never offered a letter or a digit, so the flag's minutes and the words
// beside it keep the face the rest of the display uses. These readings pin the family, the range that
// limits it, the fact that it is a font already on the machine rather than one downloaded for a
// single glyph, and the flag rule that puts it ahead of the page's text face.

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, it } from 'node:test';

const STYLESHEET = join(dirname(fileURLToPath(import.meta.url)), '..', 'styles', 'rdio.css');

const HOURGLASS = 'U+29D6';
const SYMBOL_FONTS = ['Noto Sans Symbols 2', 'Segoe UI Symbol', 'Apple Symbols', 'Symbola'];

const blanked = (text) => text.replace(/\/\*[\s\S]*?\*\//g, '');
const flat = (raw) => raw.replace(/\s+/g, '');

const declarations = (body) => {
  const out = {};

  for (const entry of body.split(';')) {
    const pair = entry.split(':');

    if (pair.length === 2) out[pair[0].trim()] = pair[1].trim();
  }

  return out;
};

// The top-level rules, read the way the browser reads them: comments blanked, media blocks taken out
// so a resting reading answers the resting cascade, and each rule's own declarations. The reader the
// other stylesheet suites carry skips selectors beginning with `@`, so the declared font families are
// read separately below.
function rules(text) {
  const top = blanked(text).replace(/@media[^{]+\{(?:[^{}]|\{[^{}]*\})*\}/g, '');

  return [...top.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
    .map((match) => ({ selector: match[1].trim(), declarations: declarations(match[2]) }))
    .filter((rule) => !rule.selector.startsWith('@'));
}

function fontFaces(text) {
  return [...blanked(text).matchAll(/@font-face\s*\{([^{}]*)\}/g)]
    .map((match) => declarations(match[1]));
}

const SHEET = () => readFileSync(STYLESHEET, 'utf8');

const face = (family) => fontFaces(SHEET()).find((entry) => flat(entry['font-family'] ?? '') === flat(family));

// The declarations the flag rule leaves on the element, merged in cascade order.
function declared(selector) {
  const out = {};

  for (const rule of rules(SHEET())) {
    if (rule.selector.split(',').map(flat).includes(flat(selector))) Object.assign(out, rule.declarations);
  }

  return out;
}

describe('the listener stylesheet — the font that draws the timed-avoid hourglass', () => {
  it('declares a family for the hourglass alone', () => {
    const declaredFace = face('"rdio-symbol"');

    assert.ok(declaredFace, 'the listener stylesheet declares no rdio-symbol family, so the hourglass is left to whatever the browser falls back to');

    const ranges = (declaredFace['unicode-range'] ?? '').split(',').map((part) => part.trim());

    assert.deepEqual(ranges, [HOURGLASS],
      `the rdio-symbol family is offered for more than the hourglass: ${ranges.join(', ')}`);
  });

  it('draws that family from a font already on the machine', () => {
    const declaredFace = face('"rdio-symbol"');

    assert.ok(declaredFace, 'the listener stylesheet declares no rdio-symbol family to draw the hourglass');

    const sources = (declaredFace.src ?? '').split(',').map((part) => part.trim());

    assert.ok(sources.length > 0 && sources.every((source) => source.startsWith('local(')),
      `the rdio-symbol family downloads a font for one glyph: ${declaredFace.src}`);

    for (const family of SYMBOL_FONTS) {
      assert.ok(sources.some((source) => flat(source) === flat(`local("${family}")`)),
        `the rdio-symbol family does not name ${family}, which carries the hourglass on the platform it runs on`);
    }
  });

  it('offers that family to the flags ahead of the page\'s text face', () => {
    const stack = declared('rdio-display .flag')['font-family'];

    assert.ok(stack, 'the flag rule names no font stack, so the hourglass is drawn by whatever the browser resolves');
    assert.ok(flat(stack).startsWith(flat('"rdio-symbol"')),
      `the flag rule puts something before the hourglass family: ${stack}`);
    assert.ok(flat(stack).includes(flat('var(--rdio-font-family)')),
      `the flag rule does not carry the page's own text face after the hourglass family: ${stack}`);
  });
});
