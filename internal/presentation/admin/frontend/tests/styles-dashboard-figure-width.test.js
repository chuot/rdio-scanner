// the dashboard figures' values: a value too long for the room its row leaves it breaks inside the value,
// so the figures column, the card, and the grid track the card sits in stay the width the layout gave them
// the sheet is read as written: the shape it declares, not what a browser resolves

import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const stylesDir = join(dirname(fileURLToPath(import.meta.url)), '..', 'styles');

const adminCss = () => readFileSync(join(stylesDir, 'admin.css'), 'utf8');

// The sheet the page's sizes are named in: the cap the cards are capped with and the gap a figure row keeps
// are both written there, and a test that holds them holds the numbers the layout was measured with.
const themeCss = () => readFileSync(join(stylesDir, '..', '..', '..', 'common', 'frontend', 'styles', 'theme.css'), 'utf8');

// The path a card's figures list gives a value: the element, not a longer selector that reaches through it,
// and not the class the markup happens to carry on it.
const FIGURE_VALUE = /\.rdio-dashboard-figures\s+dd\b/;
const FIGURE_CAPTION = /\.rdio-dashboard-figures\s+dt\b/;
const FIGURE_ROW = /\.rdio-dashboard-figures\s+div\b/;
const FIGURE_CLASS = /\.rdio-dashboard-figure\b/;

// The grid the cards sit in, the card itself, and any rule that speaks to a dashboard card or to that grid.
const CARDS_GRID = /\.rdio-dashboard-cards$/;
const ADMIN_CARD = /\.rdio-admin-card$/;
const ANY_DASHBOARD_CARD = /\.rdio-dashboard\b/;

// The media block the wider column starts at: 720 px is where the sheet's two narrow blocks are both behind it,
// so a rule read there cannot reach the widths the narrow group owns.
const WIDE_BOUNDARY = 'min-width: 720px';

// A declaration that gives a box its own width, or hides what it cannot draw, instead of letting a line
// break: each one either raises the minimum the card is measured at or moves part of the value out of reach.
const WIDTH_OR_SCROLL = /(^|[;\s])(min-width|max-width|width|overflow|overflow-x|text-overflow)\s*:/i;

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

// The rules the media blocks carry, at the same level the top-level rules are read at. With a condition, only
// the block that names it is read: the shape a sheet hands the page from one width up is a different rule from
// the one it hands at every width, and a guard that reads both cannot say which width each one answers.
function mediaRules(css, condition) {
  return blocksAtOneLevel(css)
    .filter((block) => block.selector.startsWith('@media'))
    .filter((block) => !condition || tidy(block.selector).includes(condition))
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

// The size a token is given in the theme sheet, or null when the sheet stops naming it.
function tokenValue(css, token) {
  const match = css.match(new RegExp(`(^|\\s)${token}\\s*:\\s*([^;]+)`, 'm'));

  return match ? match.at(-1).trim().replace(/\s+/g, ' ') : null;
}

// The word break a rule asks for, and the property it asks it with: `overflow-wrap` breaks a word only when
// the line cannot hold it, `word-break` breaks words at every break opportunity, including the spaces a
// serving folder path carries between its separators. Only a break with the value `anywhere` fires inside a
// run of characters that carries no space, hyphen, or slash to break at, which is what a folder path put
// under a user profile, or a version string, is made of.
function breakOf(rule) {
  const overflowWrap = declared(rule.block, 'overflow-wrap');

  if (overflowWrap) return { property: 'overflow-wrap', value: overflowWrap };

  const wordBreak = declared(rule.block, 'word-break');

  if (wordBreak) return { property: 'word-break', value: wordBreak };

  return null;
}

function rulesMatching(rules, pattern) {
  return rules.filter((rule) => pattern.test(rule.selector));
}

const quoted = (rules) => rules.map((rule) => `"${rule.selector} { ${tidy(rule.block)} }"`).join(', ');

function readFigureValues(css) {
  const found = rulesMatching(topRules(css), FIGURE_VALUE);

  assert.equal(found.length, 1,
    `the sheet reaches a dashboard figure's value with ${found.length} rules outside its media blocks (${quoted(found)}): the break that keeps a figures column the width its card was given has to be declared once, on the value itself`);

  return found[0];
}

test('a figure value too long for the room its row leaves it breaks inside the value', () => {
  const rule = readFigureValues(adminCss());
  const wordBreak = breakOf(rule);

  assert.ok(wordBreak,
    `no rule gives a dashboard figure's value a break inside its own word: "${rule.selector}" carries ${tidy(rule.block)}, and nothing in the sheet gives a figure's value an overflow-wrap, so the value is drawn at overflow-wrap: normal. The figures list sizes its one column by the widest row it holds, so a serving folder too long for the room its row leaves it widens that column, the card's own minimum, and the grid track the card sits in, and the value is drawn past the edge of its card and over the card beside it`);

  assert.equal(wordBreak.value, 'anywhere',
    `a figure's value breaks at overflow-wrap: ${wordBreak.value} (${rule.selector}): a break that only fires at a hyphen or at a slash leaves a serving folder such as a programs directory under a user profile, or a version string such as a release candidate tag, unbroken at every width the page is used at`);

  assert.equal(wordBreak.property, 'overflow-wrap',
    `a figure's value is broken by ${wordBreak.property}: ${wordBreak.value} (${rule.selector}): a break that fires at every break opportunity splits the words that do carry spaces, so a caption and its value read as two lines where a value is simply too long for its row`);

  assert.doesNotMatch(rule.selector, /\[[^\]]*=[^\]]*\]/,
    `the break is given to figure values by "${rule.selector}", which names one card by an attribute: every card's figures list sizes its column the same way, so each of them needs the break and not only the one a report happened to name`);

  assert.doesNotMatch(rule.selector, /:nth-child\(/,
    `the break is given to figure values by "${rule.selector}", which names a position in a card's markup: which row carries a serving folder is what the server's payload decides, so a break tied to a position moves with the figures a card prints`);
});

test('a figure value is given no width, no minimum, and no hidden overflow to fit with', () => {
  const css = adminCss();

  const widened = rulesMatching(topRules(css).concat(mediaRules(css)), FIGURE_VALUE)
    .filter((rule) => WIDTH_OR_SCROLL.test(`${rule.block};`));

  assert.deepEqual(widened.map((rule) => `${rule.selector} { ${tidy(rule.block)} }`), [],
    `a rule gives a figure's value a width or a hidden overflow (${quoted(widened)}): a minimum width raises the card's own minimum above the track the layout gives it, and a scroll on the value's box hides the rest of a serving folder behind an edge an operator has to scroll to reach`);

  const scoped = rulesMatching(topRules(css).concat(mediaRules(css)), FIGURE_VALUE)
    .filter((rule) => /\[[^\]]*=[^\]]*\]/.test(rule.selector) || /:nth-child\(/.test(rule.selector));

  assert.deepEqual(scoped.map((rule) => rule.selector), [],
    `a rule reaches figure values through a card attribute or a child position (${scoped.map((rule) => `"${rule.selector}"`).join(', ')}): the figures of every dashboard card are laid out the same way, so the break cannot belong to one card or to wherever a row happens to sit in a card's markup`);
});

test('the cards grid keeps the shipped floor and scales it from 720 px up', () => {
  const css = adminCss();

  const rules = rulesMatching(topRules(css), CARDS_GRID);

  assert.equal(rules.length, 1, `the sheet carries ${rules.length} rules for the cards grid itself`);

  const [grid] = rules;
  const columns = declared(grid.block, 'grid-template-columns');
  const gap = declared(grid.block, 'gap');

  assert.ok(columns, `the cards grid declares no columns: ${quoted(rules)}`);
  assert.equal(columns, 'repeat(auto-fit, minmax(280px, 1fr))',
    `the cards grid's plain rule now reads ${columns}: that rule is what lays the cards out at every width its wider rule is not read at, so changing it changes the cards at the widths this feature leaves as they are`);
  assert.match(columns, /auto-fit/,
    `the cards grid counts ${columns}, which is no longer the number of columns the room the card has decides`);
  assert.equal(gap, 'var(--rdio-space-3)', 'the cards grid no longer sits on the gap its tracks were measured with');

  const wide = rulesMatching(mediaRules(css, WIDE_BOUNDARY), CARDS_GRID);

  assert.equal(wide.length, 1, `the sheet carries ${wide.length} rules for the cards grid in the block that starts at 720 px: the room the column gives a card past the shipped 934 px box is answered there or nowhere, and a wide column that leaves the grid to the plain rule lays the cards out at a floor chosen for a 900 px column`);

  const scaled = declared(wide[0].block, 'grid-template-columns');

  assert.ok(scaled, `the cards grid's rule at 720 px and above declares no columns: ${quoted(wide)}`);
  assert.equal(scaled, 'repeat(auto-fit, minmax(max(280px, calc((100% - 2 * var(--rdio-space-3)) / 3)), 1fr))',
    `the cards grid's floor from 720 px up is ${scaled ?? 'nothing'}: a floor that does not scale with the room the column gives leaves the fit unconstrained, which seats four narrow cards beside one another in a wide column instead of three cards an operator reads across`);
  assert.match(scaled, /max\(\s*280px\s*,/,
    `the scaled floor starts at ${scaled}: the 280 px half still binds from 720 px to 1199 px, where three tracks of it do not fit the room yet, and a floor that drops it seats cards narrower than the shipped grid ever seated one`);
});

test('the cap every administration card is drawn under answers the room it is given', () => {
  const rules = rulesMatching(topRules(adminCss()), ADMIN_CARD);

  assert.equal(rules.length, 1, `the sheet carries ${rules.length} rules for a card itself`);

  const [card] = rules;
  const cap = declared(card.block, 'max-width');

  assert.equal(cap, 'var(--rdio-content-width)',
    `a card is now drawn under ${cap ?? 'no cap'}: the room a figure value has to break inside is decided by the width the page is laid out at, so a card that stops being capped either stops telling the layout where its edge is or grows past the column the page was built around`);

  const capValue = tokenValue(themeCss(), '--rdio-content-width');

  assert.equal(capValue, 'min(1400px, 100%)',
    `the cap the cards are capped with is now ${capValue}: a cap frozen at one width leaves the room a wider window gives unread beside the cards — at 2560 px the shipped 900px cap fills 934 px of a 2308 px room, leaving 687 px at each edge of it — and the column has to answer the room it is given up to the width it stops growing at`);
});

test('no dashboard card and neither the cards grid nor the dashboard section is handed a sideways scroll', () => {
  const css = adminCss();

  const scrollers = topRules(css)
    .concat(mediaRules(css))
    .filter((rule) => ANY_DASHBOARD_CARD.test(rule.selector))
    .filter((rule) => declared(rule.block, 'overflow-x') || declared(rule.block, 'overflow'));

  assert.deepEqual(scrollers.map((rule) => `${rule.selector} -> ${declared(rule.block, 'overflow-x') ?? declared(rule.block, 'overflow')}`), [],
    `a dashboard card or a container holding the cards is given a scroll (${quoted(scrollers)}): the value stays wider than the room the layout gives the card, and an operator reading a card has to scroll sideways to see the rest of each figure`);
});

test('the class the markup puts on a figure value is not the hook the sheet hangs the break on', () => {
  const css = adminCss();

  const named = rulesMatching(topRules(css).concat(mediaRules(css)), FIGURE_CLASS);

  assert.deepEqual(named.map((rule) => rule.selector), [],
    `the sheet styles "${named.map((rule) => `"${rule.selector}"`).join(', ')}": the class a card's markup carries on one kind of value is not what every figures list gives its values, so a rule hung on it leaves the other cards' figures unbroken and breaks nothing that the row itself decides`);
});

test('a caption stays on the left of its value, and the value stays right-aligned in the mono face', () => {
  const css = adminCss();

  const rows = rulesMatching(topRules(css), FIGURE_ROW);

  assert.equal(rows.length, 1, `the sheet carries ${rows.length} rules for the row a caption and its value are laid out in`);

  const [row] = rows;

  assert.equal(declared(row.block, 'justify-content'), 'space-between',
    `a figure row is now laid out with ${declared(row.block, 'justify-content')}: the caption and the value are read across the card from opposite edges, which is what leaves the value the room its own line has to break inside`);
  assert.equal(declared(row.block, 'gap'), 'var(--rdio-space-2)', 'a figure row no longer keeps the gap its measurements were taken with');
  assert.equal(tokenValue(themeCss(), '--rdio-space-2'), '8px', 'the gap a figure row keeps is no longer the 8px the caption and the value were read with');

  const values = rulesMatching(topRules(css), FIGURE_VALUE);

  assert.equal(values.length, 1, `the sheet carries ${values.length} rules for a figure's value itself`);

  const [value] = values;

  assert.equal(declared(value.block, 'text-align'), 'right',
    `a figure's value is now aligned ${declared(value.block, 'text-align') ?? 'the other way'}: the digits of one card are read lined up with the digits of the card beside it, and a value that stops ending at the same edge breaks that reading across the whole row of cards`);
  assert.equal(declared(value.block, 'font-family'), 'var(--rdio-mono)',
    `a figure's value is now set in ${declared(value.block, 'font-family') ?? 'no family'}: a proportional face moves the digits of one figure against the digits of the figure under it`);

  const captions = rulesMatching(topRules(css), FIGURE_CAPTION);

  assert.equal(captions.length, 1, `the sheet carries ${captions.length} rules for a figure's caption`);

  const captionSize = declared(captions[0].block, 'font-size');
  const valueSize = declared(value.block, 'font-size');

  assert.equal(valueSize, captionSize,
    `a figure is set at ${valueSize} against its caption's ${captionSize}: the value is the figure and the caption is the smaller line naming it, so the two sizes going apart reads as a value with a note under it instead of a figure named on its left`);
});
