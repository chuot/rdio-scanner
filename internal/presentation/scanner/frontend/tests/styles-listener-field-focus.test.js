// the search and unlock fields' keyboard focus: the visible field is
// the box, the control inside it is inset, and a ring drawn on the
// control frames the control, not the field; the ring belongs on the
// box — around it for the filter fields, inside it for the unlock field.
// see the field-focus-ring-on-box maintainer note.

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const STYLESHEET = join(dirname(fileURLToPath(import.meta.url)), '..', 'styles', 'rdio.css');

// The cascade, read the way the browser reads it: comments blanked, the top-level rules, and
// each media block's inner rules tagged with the condition that runs them.
function cascade(text) {
  const blanked = text.replace(/\/\*[\s\S]*?\*\//g, '');
  const rules = [];

  const parse = (source, media) => {
    for (const match of source.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
      const selector = match[1].trim();

      if (selector.startsWith('@')) {
        continue;
      }

      const declarations = {};

      for (const entry of match[2].split(';')) {
        const pair = entry.split(':');

        if (pair.length === 2) {
          declarations[pair[0].trim()] = pair[1].trim();
        }
      }

      rules.push({ selector, declarations, media: media === null ? null : flat(media) });
    }
  };

  const mediaBlocks = [...blanked.matchAll(/@media([^{]+)\{((?:[^{}]|\{[^{}]*\})*)\}/g)];
  const top = blanked.replace(/@media[^{]+\{(?:[^{}]|\{[^{}]*\})*\}/g, '');

  parse(top, null);

  for (const block of mediaBlocks) {
    parse(block[2], block[1].trim());
  }

  return rules;
}

const SHEET = () => readFileSync(STYLESHEET, 'utf8');
const RULES = () => cascade(SHEET());

// Values compare as the browser sees them: `2px solid #424242` and `2px solid  #424242` are the
// same ring.
const flat = (raw) => raw.replace(/\s+/g, '');
const selectors = (rule) => rule.selector.split(',').map(flat);

// Every rule whose selector list carries the target selector, with its declarations merged in
// cascade order — the reading a focused field leaves on the element.
function declared(selector, media = null) {
  const out = {};

  for (const rule of RULES()) {
    if (rule.media === media && selectors(rule).includes(flat(selector))) Object.assign(out, rule.declarations);
  }

  return out;
}

function expect(selector, property, want, media = null) {
  const got = declared(selector, media)[property];

  assert.equal(
    got === undefined ? undefined : flat(got),
    flat(want),
    `the stylesheet draws ${selector} ${property}: ${got}, not ${want}`,
  );
}

// The weight a selector carries: ids, then classes, attributes and pseudo-classes, then element
// names and pseudo-elements. The universal selector and the combinators add nothing.
const weight = (selector) => {
  const s = selector.trim();
  const ids = (s.match(/#[\w-]+/g) ?? []).length;
  const classes = (s.match(/\.[\w-]+|\[[^\]]+\]|:(?!:)[\w-]+/g) ?? []).length;
  const bare = s.replace(/#[\w-]+|\.[\w-]+|\[[^\]]+\]|::[\w-]+(\([^)]*\))?|:[\w-]+(\([^)]*\))?/g, ' ');
  const elements = (bare.match(/[a-z][\w-]*/gi) ?? []).length + (s.match(/::[\w-]+/g) ?? []).length;

  return [ids, classes, elements];
};

const heavier = (a, b) => {
  for (let i = 0; i < 3; i++) {
    if (a[i] !== b[i]) return a[i] - b[i];
  }

  return 0;
};

// What an element actually ends up with when a blanket rule reaches it as well as the rule written
// for it: the browser takes the more specific rule, and the later one between equals.
function effective(selector, blanket, media = null) {
  const hits = RULES()
    .map((rule, at) => ({ ...rule, at }))
    .filter((rule) => rule.media === media &&
      selectors(rule).some((s) => s === flat(selector) || s === flat(blanket)));

  hits.sort((a, b) => heavier(weight(a.selector), weight(b.selector)) || a.at - b.at);

  const out = {};

  for (const rule of hits) {
    if (selectors(rule).includes(flat(selector))) Object.assign(out, rule.declarations);
    else for (const [k, v] of Object.entries(rule.declarations)) if (!(k in out)) out[k] = v;
  }

  return out;
}

const FILTER_BOX = 'rdio-search-panel .rdio-filters .rdio-field-box:has(:focus-visible)';
const UNLOCK_BOX = 'rdio-display .auth .rdio-field-box:has(:focus-visible)';

const FIELD_CONTROLS = [
  'rdio-search-panel .rdio-filters rdio-form-field select:focus-visible',
  'rdio-search-panel .rdio-filters rdio-form-field input:focus-visible',
  'rdio-display .auth rdio-form-field input:focus-visible',
];

describe('the field\'s keyboard ring — drawn on the field box', () => {
  it('draws the ring on the filter field box, at the shape and distance every other control keeps', () => {
    expect(FILTER_BOX, 'outline', '2px solid #424242');
    expect(FILTER_BOX, 'outline-offset', '2px');
  });

  it('draws the ring inside the unlock field box\'s own edge, and none outside it', () => {
    expect(UNLOCK_BOX, 'box-shadow', 'inset 0 0 0 2px #424242');
    expect(UNLOCK_BOX, 'outline', 'none');
  });

  it('leaves the control inside the field no ring and no line of its own', () => {
    for (const control of FIELD_CONTROLS) {
      const drawn = declared(control);

      assert.equal(flat(drawn.outline ?? 'none'), 'none',
        `the control ${control} still draws a ring of its own: ${drawn.outline}`);
      assert.equal(drawn['border-bottom'], undefined,
        `the control ${control} still draws a border-bottom beside the field's underline`);
    }
  });
});

describe('the field\'s keyboard ring under a forced palette', () => {
  const FORCED = '(forced-colors:active)';

  it('keeps the ring on the field box in the colour the system uses to say here', () => {
    expect(FILTER_BOX, 'outline', '2px solid Highlight', FORCED);
    expect(FILTER_BOX, 'outline-offset', '2px', FORCED);
    expect(UNLOCK_BOX, 'outline', '2px solid Highlight', FORCED);
    expect(UNLOCK_BOX, 'outline-offset', '2px', FORCED);
  });

  it('leaves the control inside the field no ring of its own in the mode either', () => {
    for (const control of FIELD_CONTROLS) {
      assert.equal(declared(control, FORCED).outline, undefined,
        `the control ${control} still carries a ring in the forced-colours block`);
    }
  });
});

describe('the field\'s other focus behaviour — unchanged while the ring moves', () => {
  it('keeps the underline the pointer and keyboard thicken on the box', () => {
    for (const line of [
      'rdio-search-panel .rdio-filters .rdio-field-box:focus-within::after',
      'rdio-display .auth .rdio-field-box:focus-within::after',
    ]) {
      expect(line, 'background-color', '#424242');
      expect(line, 'height', '2px');
    }
  });

  it('keeps the select arrow turning while the field holds focus', () => {
    expect('rdio-search-panel .rdio-filters .rdio-field-box:focus-within::before', 'transform', 'rotate(180deg)');
  });
});

describe('the field\'s keyboard ring — drawn whole, not cut off', () => {
  // the forced-colours ring is drawn outside the field box, so it only
  // reads as the field's focus indicator while nothing nearer paints
  // over it: the display clips everything inside itself, and the card
  // carrying the unlock field hugs that field exactly. (The normal-mode
  // ring on this field is inset and needs no release; the filter
  // fields' normal-mode ring is outside and rides no clipping chain.)
  const BLANKET = 'rdio-display *';

  for (const wrapper of ['rdio-display .auth form', 'rdio-display .auth form rdio-form-field']) {
    it(`lets the ring out of ${flat(wrapper)} inside the display's own clipping`, () => {
      assert.equal(BLANKET && declared(BLANKET).overflow, 'hidden',
        'the display no longer clips what sits inside it, so this guard has nothing to pin');

      const got = effective(wrapper, BLANKET).overflow;

      assert.equal(got === undefined ? undefined : flat(got), 'visible',
        `${wrapper} clips the field's ring: the cascade leaves it overflow: ${got ?? 'hidden'}`);
    });
  }
});
