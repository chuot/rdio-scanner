// the dashboard's Busiest talkgroups table: the two name columns break a word that is too long for the
// column they are given, the tally column and the headers do not, and the table keeps the layout that sizes
// each column to the content it holds
// the sheet is read as written: the shape it declares, not what a browser resolves

import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const stylesDir = join(dirname(fileURLToPath(import.meta.url)), '..', 'styles');

const adminCss = () => readFileSync(join(stylesDir, 'admin.css'), 'utf8');

// The table, and the cells that hold a talkgroup name or a system label. The name cells are named by what they
// are not — the last cell of a row is the tally — so the rule keeps holding them when a column is added to the
// left of the tally, and it cannot reach the tally itself.
const TABLE = /\.rdio-dashboard-busiest$/;
const NAME_CELL = /\.rdio-dashboard-busiest td:not\(:last-child\)$/;

// Any rule that speaks to this table, its cells, or the cards the table sits in.
const ANY_OF_THIS_TABLE = /\.rdio-dashboard-busiest\b/;
const CARDS = /\.rdio-dashboard\b/;

// A selector that names a header cell, or the last cell of a row, or a table cell as a group: each of those
// reaches either a header's own word or the count the five rows are ranked by.
const HEAD_OR_ANY_CELL = /\b(th\b|td:last-child|td$|td,)/;

// A long talkgroup name or system label is one word with no space in it: call metadata writes names that way,
// and an operator can name a system that way. The automatic table layout gives a column at least the width of
// the longest word it holds, so such a word decides how wide the whole table is, and the table grows past the
// card that holds it. Breaking inside the word is what keeps the table the width the card is given; a break at
// a hyphen or a slash is not enough, because these words carry neither.
const BREAKS_INSIDE_A_WORD = /anywhere|break-word/;

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

// The rules every media block carries, at the same level the top-level rules are read at.
function mediaRules(css) {
  return blocksAtOneLevel(css)
    .filter((block) => block.selector.startsWith('@media'))
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

// The word break a rule asks for, and the property it asks it with: `overflow-wrap` breaks a word only when the
// line cannot hold it, `word-break` breaks words at every break opportunity, including the spaces an operator
// put in a name.
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

function readNameCells(css) {
  const found = rulesMatching(topRules(css), NAME_CELL);

  assert.equal(found.length, 1,
    `nothing caps the width the busiest table's name columns may break inside: no rule reads "${NAME_CELL.source}" among the sheet's rules outside its media blocks, so a talkgroup name or a system label written as one word decides the table's width, and the table is drawn past the card that holds it`);

  const [rule] = found;
  const wordBreak = breakOf(rule);

  assert.ok(wordBreak, `the name cells declare no word break: ${quoted(found)}`);

  return { rule, ...wordBreak };
}

test('a name too long for its column breaks inside that column', () => {
  const { rule, property, value } = readNameCells(adminCss());

  assert.match(value, BREAKS_INSIDE_A_WORD,
    `the name cells break at ${property}: ${value} (${rule.selector}): a talkgroup name or a system label written as one word finds no break opportunity inside its own column, so the word's own width becomes the column's minimum and the whole table is as wide as the longest word in it`);

  assert.equal(property, 'overflow-wrap',
    `the name cells are broken by ${property}: ${value} (${rule.selector}): a break that fires at every break opportunity splits the names that do carry spaces, so the card reads "Dispat"/"ch One" where it should print a name that is simply too long for its column`);
});

test('the break stops at the two name columns, and never reaches a header or the tally', () => {
  const css = adminCss();

  const reached = topRules(css)
    .concat(mediaRules(css))
    .filter((rule) => ANY_OF_THIS_TABLE.test(rule.selector))
    .filter((rule) => breakOf(rule))
    .filter((rule) => !NAME_CELL.test(rule.selector) || HEAD_OR_ANY_CELL.test(rule.selector));

  assert.deepEqual(reached.map((rule) => `${rule.selector} -> ${breakOf(rule).property}: ${breakOf(rule).value}`), [],
    `a word break reaches a header cell, the tally cell, or the table's cells as a group (${quoted(reached)}): a header whose own word is split no longer names its column in one piece, and a count printed on two lines is a count that cannot be compared down the column the five rows are ranked by`);

  const { rule } = readNameCells(css);

  assert.match(rule.selector, /:not\(:last-child\)/,
    `the name columns are given the break by "${rule.selector}", which reaches the last cell of a row: the tally has to keep its digits whole (${quoted([rule])})`);
});

test('the table keeps the layout that sizes each column to the content it holds', () => {
  const rules = rulesMatching(topRules(adminCss()), TABLE);

  assert.equal(rules.length, 1, `the sheet carries ${rules.length} rules for the busiest table itself`);

  const layout = declared(rules[0].block, 'table-layout');

  assert.ok(layout === null || tidy(layout) === 'auto',
    `the busiest table is laid out as ${layout} (${quoted(rules)}): a fixed layout gives every column a width chosen in the sheet instead of the width its own content asks for, so the name columns are squeezed to nothing while the tally column is padded out at every viewport the sheet does not restate the column widths for`);
});

test('the overflow is made to fit the card, not handed to a sideways scroll', () => {
  const scrollers = topRules(adminCss())
    .concat(mediaRules(adminCss()))
    .filter((rule) => CARDS.test(rule.selector) && declared(rule.block, 'overflow-x'));

  assert.deepEqual(scrollers.map((rule) => `${rule.selector} -> overflow-x: ${declared(rule.block, 'overflow-x')}`), [],
    `a dashboard card or the cards grid is given a sideways scroll (${quoted(scrollers)}): the table stays wider than the room the layout gives the card, and a person reading the busiest talkgroups has to scroll sideways to see the rest of each row`);
});
