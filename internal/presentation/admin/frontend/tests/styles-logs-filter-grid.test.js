// the Logs filter form's grid: a column count that answers the room the card has, and the two columns
// the narrow screens were given on purpose
// the sheet is read as written: the shape it declares, not what a browser resolves

import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const stylesDir = join(dirname(fileURLToPath(import.meta.url)), '..', 'styles');

const adminCss = () => readFileSync(join(stylesDir, 'admin.css'), 'utf8');

// The rule that lays out the form, the rule that fills the fields, and the rule that carries the action
// row: each is the selector that ends on that element, not a longer one that reaches through it.
const FORM = /\.rdio-filters form$/;
const FIELD = /\.rdio-filters rdio-form-field$/;
const ACTIONS = /\.rdio-filters \.actions$/;

// The last viewport width below the point where the room-sized rule stops holding two columns, put there
// by the track readings the harness took: at 730 px the card lays out 444 px, which is two 216 px tracks
// and one 12 px gap and no more.
const NARROW_BOUNDARY = '729.98px';
const NARROW_TRACKS = 'repeat(2, minmax(0, 260px))';

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

const tidy = (text) => text.trim().replace(/\s+/g, ' ');

// Rules outside every media block: the shape that holds at the widest screens.
function topRules(css) {
  return blocksAtOneLevel(css)
    .filter((block) => !block.selector.startsWith('@media'))
    .map((block) => ({ selector: tidy(block.selector), block: block.body }));
}

// The rules one media condition carries.
function mediaRules(css, condition) {
  return blocksAtOneLevel(css)
    .filter((block) => block.selector.startsWith('@media') && tidy(block.selector).includes(condition))
    .flatMap((block) => blocksAtOneLevel(block.body))
    .map((block) => ({ selector: tidy(block.selector), block: block.body }));
}

// The value one property carries in a declaration block, or null when it declares none.
// The value is always the last capture group: a property pattern's own optional group sits before it.
function declared(block, property) {
  const pattern = new RegExp(`(^|[;\\s])${property}\\s*:\\s*([^;]+)`, 'i');
  const match = block.match(pattern);

  return match ? match.at(-1).trim().replace(/\s+/g, ' ') : null;
}

// The arguments of a call, split on the commas that sit outside every call inside it, so a track built
// out of max(216px, calc(...)) is still one argument.
function callArguments(value, name) {
  const head = new RegExp(`^${name}\\(`);

  if (!head.test(value)) return null;

  const inner = value.slice(value.indexOf('(') + 1, value.lastIndexOf(')'));

  const parts = [];
  let depth = 0;
  let current = '';

  for (const character of inner) {
    if (character === '(') depth += 1;
    if (character === ')') depth -= 1;

    if (character === ',' && depth === 0) {
      parts.push(tidy(current));
      current = '';

      continue;
    }

    current += character;
  }

  parts.push(tidy(current));

  return parts;
}

function ruleFor(rules, pattern) {
  return rules.find((rule) => pattern.test(rule.selector));
}

function readForm(css) {
  const rule = ruleFor(topRules(css), FORM);

  assert.ok(rule, 'nothing in the sheet lays out the Logs filter form columns');

  const columns = declared(rule.block, 'grid-template-columns');

  assert.ok(columns, `the filter form declares no columns: "${rule.selector} { ${rule.block} }"`);

  return { rule, columns };
}

test('the filter form counts its columns by the room the card has, not by a fixed count', () => {
  const { rule, columns } = readForm(adminCss());

  const repeat = callArguments(columns, 'repeat');

  assert.ok(repeat, `the filter form's columns are not a repeat call (${columns}): "${rule.selector}"`);
  assert.equal(repeat.length, 2, `the columns repeat call carries ${repeat.length} arguments, not a count and a track`);

  assert.equal(repeat[0], 'auto-fit',
    `the filter form counts ${repeat[0]} columns: a fixed number, so the room the card's own width cap leaves beside the tracks is never used`);

  assert.doesNotMatch(repeat[0], /^\d+$/, `the filter form names ${repeat[0]} as the column count, which is a fixed count`);
  assert.doesNotMatch(columns, /260px/, `the filter form caps its tracks at a fixed width (${columns}), so a wider card buys nothing`);
});

test('the fit keeps a 216 px floor per field and never resolves more than three columns', () => {
  const { columns } = readForm(adminCss());

  const repeat = callArguments(columns, 'repeat') ?? [];
  const minmax = callArguments(repeat[1] ?? '', 'minmax');

  assert.ok(minmax, `the filter form's track is not a minmax floor (${columns}): a field can be squeezed below the width a filter needs`);
  assert.equal(minmax.length, 2, `the minmax call carries ${minmax.length} arguments, not a floor and a growth`);

  const [floor, growth] = minmax;

  assert.match(floor, /216px/, `the floor under a filter field is ${floor}, not the 216 px the form was measured holding`);
  assert.match(floor, /^max\(/,
    `the floor is a constant (${floor}), so a card with room for four 216 px tracks resolves four columns and six fields read as two rows of three narrow ones`);
  assert.match(floor, /calc\(/, `the floor does not rise with the laid-out width (${floor}), so the column count answers the floor alone`);
  assert.match(floor, /100%/, `the rising floor is not built on the width the form lays out (${floor})`);
  assert.match(floor, /var\(--rdio-space-3\)/, `the rising floor ignores the gap the form already uses (${floor})`);
  assert.match(floor, /\/\s*3\)/, `the floor is not the share of three columns (${floor}), so nothing caps the count at three`);

  assert.equal(growth, '1fr', `a column grows to ${growth} instead of filling the row the card leaves for it`);
});

test('the form keeps its gap, its alignment, the field widths, and the action row span', () => {
  const css = adminCss();
  const { rule } = readForm(css);

  assert.equal(declared(rule.block, 'gap'), 'var(--rdio-space-3)', 'the fit moved off the gap the fields already answer with');
  assert.equal(declared(rule.block, 'align-items'), 'end', 'the fields no longer sit on the baseline the row was drawn on');

  const field = ruleFor(topRules(css), FIELD);

  assert.ok(field, 'nothing keeps the filter fields filling the tracks they are given');
  assert.equal(declared(field.block, 'width'), '100%', 'a filter field no longer fills its track, so the columns do not read as a row of fields');

  const actions = ruleFor(topRules(css), ACTIONS);

  assert.ok(actions, 'nothing keeps the action row across the filter form');
  assert.equal(declared(actions.block, 'grid-column'), '1 / -1', 'the action row no longer spans every column the form resolves');
  assert.equal(declared(actions.block, 'justify-content'), 'flex-end', 'the action row buttons no longer sit at the right hand edge');
});

test('the narrow screens state the two columns they were given, at the width the fit stops holding them', () => {
  const css = adminCss();

  const block = blocksAtOneLevel(css)
    .find((candidate) => candidate.selector.startsWith('@media') && candidate.selector.includes(`max-width: ${NARROW_BOUNDARY}`));

  assert.ok(block, `no media block sits at ${NARROW_BOUNDARY}, the width the track readings put the two-column shape below`);

  const narrow = ruleFor(mediaRules(css, `max-width: ${NARROW_BOUNDARY}`), FORM);

  assert.ok(narrow, `the filter form columns are not restated inside ${tidy(block.selector)}`);

  assert.equal(declared(narrow.block, 'grid-template-columns'), NARROW_TRACKS,
    `below ${NARROW_BOUNDARY} the filter form is not the two equal columns the narrow screens were given, it is left to a room-sized rule that resolves one column there`);
});
