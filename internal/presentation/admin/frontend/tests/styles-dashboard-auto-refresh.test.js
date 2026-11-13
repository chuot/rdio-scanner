// the dashboard's auto-refresh control: its pressed face, its resting face, its forced-colours edge
// the control's state is painted by the sheet and announced by its own words; the sheet proves the paint

import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const stylesDir = join(dirname(fileURLToPath(import.meta.url)), '..', 'styles');

const adminCss = () => readFileSync(join(stylesDir, 'admin.css'), 'utf8');

const CONTROL = '.rdio-dashboard-auto-refresh';
const PRESSED = /aria-pressed\s*=\s*(['"])true\1/;

// Top-level rules of the sheet: the selector and the declaration block it carries.
function cssRules(css) {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, '');

  return [...text.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(([, selector, block]) => ({
    selector: selector.trim().replace(/\s+/g, ' '),
    block: block.trim(),
  }));
}

// Blocks at one nesting level, so a media block's own body can be read rule by rule.
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

function forcedColorRules(css) {
  return blocksAtOneLevel(css)
    .filter((block) => block.selector.startsWith('@media') && /forced-colors\s*:\s*active/.test(block.selector))
    .flatMap((block) => blocksAtOneLevel(block.body))
    .map((block) => ({ selector: block.selector, block: block.body }));
}

// The value one property carries in a declaration block, or null when it declares none.
// The value is always the last capture group: a property pattern's own optional group sits before it.
function declared(block, property) {
  const pattern = new RegExp(`(^|[;\\s])${property}\\s*:\\s*([^;]+)`, 'i');
  const match = block.match(pattern);

  return match ? match.at(-1).trim().replace(/\s+/g, ' ') : null;
}

function pressedRule(rules) {
  return rules.find((rule) => rule.selector.includes(CONTROL) && PRESSED.test(rule.selector));
}

// The log source chip the pressed face is taken from: the sheet already paints that state.
function pressedChipRule(rules) {
  return rules.find((rule) => rule.selector.includes('.rdio-log-source') && PRESSED.test(rule.selector));
}

test('the sheet paints the dashboard auto-refresh control\'s pressed state by fill, edge, and ink together', () => {
  const rule = pressedRule(cssRules(adminCss()));

  assert.ok(rule,
    `nothing in the sheet styles ${CONTROL}[aria-pressed="true"]: the pressed control is painted exactly as the resting one`);

  const background = declared(rule.block, 'background(-color)?');
  const border = declared(rule.block, 'border-color');
  const ink = declared(rule.block, 'color');

  assert.ok(background && border && ink,
    `the pressed rule paints only part of the face: ${JSON.stringify({ background, border, ink })} in "${rule.selector} { ${rule.block} }"`);
});

test('the pressed control is painted with the three colours the pressed log chip already uses', () => {
  const rules = cssRules(adminCss());

  const control = pressedRule(rules);
  const chip = pressedChipRule(rules);

  assert.ok(control && chip,
    'the pressed refresh control and the pressed log chip are not both styled by their own rules');

  const shared = ['background(-color)?', 'border-color', 'color'];

  const drift = shared
    .map((property) => {
      const mine = declared(control.block, property);
      const theirs = declared(chip.block, property);

      return mine === theirs ? null : `${property}: control "${mine}", chip "${theirs}"`;
    })
    .filter(Boolean);

  assert.deepEqual(drift, [],
    `the pressed control is not the pressed chip's face: ${drift.join(' | ')}`);
});

test('the sheet gives the dashboard auto-refresh control no state-bearing colour while it is not pressed', () => {
  const resting = cssRules(adminCss())
    .filter((rule) => rule.selector.includes(CONTROL) && !PRESSED.test(rule.selector));

  const painted = resting
    .filter((rule) =>
      declared(rule.block, 'background(-color)?') ||
      declared(rule.block, 'border(-color|top|right|bottom|left)?') ||
      declared(rule.block, 'color'),
    )
    .map((rule) => `${rule.selector} { ${rule.block} }`);

  assert.deepEqual(painted, [],
    `the resting control is painted by a rule that could carry a state: ${painted.join(' | ')}`);
});

test('the forced-colours block carries the pressed control\'s edge beside the pressed chip\'s edge', () => {
  const rules = forcedColorRules(adminCss());

  const rule = pressedRule(rules);

  assert.ok(rule,
    `the forced-colours block names no pressed ${CONTROL}: in a system palette the fill and the ink are the system's, and nothing else says which state the control is in`);

  const edge = declared(rule.block, 'border');

  assert.match(edge ?? '', /^3px\s+solid\s+CanvasText$/,
    `the pressed control's forced-colours edge is not the 3px CanvasText edge the pressed chip carries: "${rule.selector} { ${rule.block} }"`);
});
