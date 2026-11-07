// the hourglass the flag row draws is in no font the listener ships:
// the declared families' ranges exclude the codepoint and the default
// Linux fallback lacks the glyph, so the cascade names a family for it.

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

// the top-level rules, read the way the browser reads them: comments
// blanked, media blocks taken out so a resting reading answers the
// resting cascade, and each rule's own declarations. the declared
// font families are read separately.
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
