// the measure the administration page holds its own paragraphs to, and the one width the sheet opens it at
// the sheet is read as written: the shape it declares, not what a browser resolves

import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const stylesDir = join(dirname(fileURLToPath(import.meta.url)), '..', 'styles');

const adminCss = () => readFileSync(join(stylesDir, 'admin.css'), 'utf8');

// The viewport the measure opens at, and the rule and value it opens with. The width is where the content
// column stops growing, which is where a paragraph first gets long enough for a measure to be the difference
// between a line an operator reads and a line they scan.
const MEASURE_BOUNDARY = 'min-width: 1200px';
const MEASURE_SELECTOR = 'rdio-admin .rdio-admin-card p';
const MEASURE_PROPERTY = 'max-width';
const MEASURE_VALUE = '70ch';

// The properties that hold one box's width, and so the only ones a prose measure can be declared on.
const WIDTH_PROPERTIES = ['width', 'max-width', 'min-width', 'inline-size', 'max-inline-size'];

// A measure named in ch: a number and the unit, anywhere inside the value.
const isChMeasure = (value) => /\d+(?:\.\d+)?ch\b/.test(value);

// The two grid tracks the sheet already names in ch: the hour column and the count column of the dashboard's
// hour rows, sized to the monospace values they hold. They are the sheet's only ch outside the prose measure,
// and they are a track width, not a measure a paragraph is read across.
const MONOSPACE_TRACKS = '5ch 9ch 1fr';

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

// The value one property carries in a declaration block, or null when it declares none. The property name is
// matched where a declaration starts, so min-width and max-width never read as a width.
function declared(block, property) {
  const pattern = new RegExp(`(^|[;\\s])${property}\\s*:\\s*([^;]+)`, 'i');
  const match = block.match(pattern);

  return match ? match.at(-1).trim().replace(/\s+/g, ' ') : null;
}

// Every rule in the sheet, each tagged with the media condition it sits under, or null for the rules that sit
// under none. A rule one condition carries is a different rule from the one the page reads at every width.
function everyRule(css, condition = null) {
  const found = [];

  for (const block of blocksAtOneLevel(css)) {
    if (block.selector.startsWith('@media')) {
      found.push(...everyRule(block.body, tidy(block.selector)));

      continue;
    }

    found.push({ selector: tidy(block.selector), block: block.body, condition });
  }

  return found;
}

// The rules that hold a paragraph: the selector ends on a p.
const namesParagraph = (selector) => /(?:^|\s)>?\s*p$/.test(selector) || /(?:^|\s)p$/.test(selector);

// Every declaration in the sheet that names a measure in ch on a property that holds a box's width, with the
// condition, the selector, the property, and the value it carries.
function measures(css) {
  return everyRule(css).flatMap((rule) =>
    WIDTH_PROPERTIES.map((property) => ({
      condition: rule.condition,
      selector: rule.selector,
      property,
      value: declared(rule.block, property),
    })),
  ).filter((measure) => measure.value !== null && isChMeasure(measure.value));
}

// The number one media condition opens at, read from its min-width, or null when the condition bounds a width
// from above instead of from below.
function opensAt(condition) {
  if (condition === null) return null;

  const match = condition.match(/min-width:\s*(\d+(?:\.\d+)?)px/);

  return match ? Number(match[1]) : null;
}

const describeMeasure = (measure) => `${measure.condition ?? 'no media condition'} { ${measure.selector} { ${measure.property}: ${measure.value} } }`;

test('the sheet holds the page\'s prose to one measure, at the width the column stops growing at', () => {
  const found = measures(adminCss());

  assert.equal(found.length, 1,
    `the sheet declares ${found.length} rules that hold a box to a measure in ch (${found.map(describeMeasure).join('; ') || 'none'}): the page's paragraphs are held to one measure the page itself declares, and a paragraph with no measure runs the whole width the content column gives it — 1 247 px of one sentence on a 2 560 px screen — while more than one measure means each section choosing its own`);

  const [measure] = found;

  assert.equal(measure.condition, `@media (${MEASURE_BOUNDARY})`,
    `the measure sits under ${measure.condition ?? 'no media block'}: it opens where the content column stops growing, so a paragraph is held to a measure at the widths that need one and left alone below them`);
  assert.equal(measure.selector, MEASURE_SELECTOR,
    `the measure is declared on ${JSON.stringify(measure.selector)}: it is the page's own paragraphs, every section card's, not one section's prose or one card's`);
  assert.equal(measure.property, MEASURE_PROPERTY,
    `the measure is declared as ${measure.property}: a floor or a fixed width under a paragraph would squeeze short paragraphs too, and a measure is only a ceiling`);
  assert.equal(measure.value, MEASURE_VALUE,
    `the measure is ${measure.value}, not the 70ch the widest paragraph on the page was measured needing: a measure narrower than that breaks a paragraph an operator reads into more lines than the card has room for, and a wider one lets the line run past the point the eye can follow back to its start`);
});

test('no block that opens below 1200 px holds a paragraph to a measure', () => {
  const found = measures(adminCss());

  for (const measure of found) {
    const width = opensAt(measure.condition);

    assert.ok(width !== null, `the measure sits under ${measure.condition ?? 'no media block'}, so it holds prose to a measure at the narrowest screens too, where a paragraph already fits the card`);
    assert.equal(width, 1200, `the measure opens at ${width} px: at the widths the column gives a card less than 934 px there is nothing for a measure to hold back, and the shape at and below 1 199 px has to stay the one the shipped page draws`);
  }

  const below = everyRule(adminCss()).filter((rule) => {
    const width = opensAt(rule.condition);

    return width !== null && width < 1200;
  });

  const carried = below.filter((rule) => WIDTH_PROPERTIES.some((property) => isChMeasure(declared(rule.block, property) ?? '')));

  assert.deepEqual(carried, [],
    `the sheet holds prose to a measure below 1 200 px (${carried.map((rule) => `${rule.condition} { ${rule.selector} }`).join('; ')}): the blocks that open at the sheet's own narrow widths already shape those screens, and a measure added under one changes a screen the page draws today`);
});

test('the measure reaches a paragraph directly, not through a card attribute, a position, or a section', () => {
  const indirect = everyRule(adminCss()).filter((rule) =>
    namesParagraph(rule.selector) &&
    (rule.selector.includes('>') || /\bdata-|:nth-|:first|:last/.test(rule.selector) ||
      /\.rdio-admin-card\.(?:dashboard|logs|systems|server|tools|options|settings)\b/.test(rule.selector)));

  const capped = indirect.filter((rule) =>
    WIDTH_PROPERTIES.some((property) => isChMeasure(declared(rule.block, property) ?? '')));

  assert.deepEqual(capped, [],
    `a paragraph reached through a card attribute, a child position, or a named section is held to a measure of its own (${capped.map((rule) => `${rule.condition} { ${rule.selector} }`).join('; ')}): one measure for the whole page is what keeps two cards side by side reading as the same column of prose`);
});

test('nothing but a paragraph is capped in ch', () => {
  const anyCh = everyRule(adminCss()).flatMap((rule) => {
    const declarations = rule.block.split(';').map(tidy).filter(Boolean);

    return declarations.filter((entry) => isChMeasure(entry)).map((entry) => ({ rule, entry }));
  });

  const unexpected = anyCh.filter(({ rule, entry }) => {
    const property = entry.split(':')[0].trim().toLowerCase();
    const value = entry.slice(entry.indexOf(':') + 1);

    if (property === MEASURE_PROPERTY && rule.selector === MEASURE_SELECTOR) return false;

    return !(property === 'grid-template-columns' && tidy(value) === MONOSPACE_TRACKS);
  });

  assert.deepEqual(unexpected, [],
    `the sheet names a measure in ch somewhere a paragraph is not (${unexpected.map(({ rule, entry }) => `${rule.selector} { ${entry} }`).join('; ')}): the one prose measure and the two monospace tracks of the dashboard hour rows are what the sheet sizes in ch, and a card, a box, or a grid track capped in ch is a width no reading of the page chose`);
});
