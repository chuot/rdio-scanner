// The main screen's picture, pinned to the v6.6.3 reading.
//
// The status light, the display rows, and the control buttons are the three surfaces the
// listener looks at all day, and their shapes are statements the stylesheet makes: the light's
// face and glow, the eight lit colours, the paused blink, the rows' space-between placement,
// and the beveled buttons' bevel, pressed dip, and status dots. This suite reads the cascade
// the browser is given — comments blanked, each rule's own declarations — and pins those
// statements to the values the v6.6.3 client drew, so a later change to the screen's look is a
// decision, not a drift.

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

function keyframes(text, name) {
  const blanked = text.replace(/\/\*[\s\S]*?\*\//g, '');
  const block = blanked.match(new RegExp(`@keyframes\\s+${name}\\s*\\{((?:[^{}]|\\{[^{}]*\\})*)\\}`));

  if (!block) return null;

  const stops = {};

  for (const stop of block[1].matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const declarations = {};

    for (const entry of stop[2].split(';')) {
      const pair = entry.split(':');

      if (pair.length === 2) {
        declarations[pair[0].trim()] = pair[1].trim();
      }
    }

    stops[stop[1].trim()] = declarations;
  }

  return stops;
}

const SHEET = () => readFileSync(STYLESHEET, 'utf8');
const RULES = () => cascade(SHEET());

// Values compare as the browser sees them: `rgb(0, 230, 118)` and `rgb(0,230,118)` are the
// same colour, and one space inside a shadow is the same shadow.
const flat = (raw) => raw.replace(/\s+/g, '');
const selectors = (rule) => rule.selector.split(',').map(flat);

// Every rule whose selector list carries the target selector, with its declarations merged in
// cascade order — the reading a control's own block leaves on the element. Media rules are the
// cascade under a condition the platform may or may not run, so the resting reading takes the
// top-level rules alone; a media block's own rules are read by naming its condition.
function declared(selector, media = null) {
  const out = {};

  for (const rule of RULES()) {
    if (rule.media === media && selectors(rule).includes(flat(selector))) Object.assign(out, rule.declarations);
  }

  return out;
}

function has(selector, media = null) {
  return RULES().some((rule) => rule.media === media && selectors(rule).includes(flat(selector)));
}

function expect(selector, property, want, media = null) {
  const got = declared(selector, media)[property];

  assert.equal(
    got === undefined ? undefined : flat(got),
    flat(want),
    `the stylesheet draws ${selector} ${property}: ${got}, not ${want}`,
  );
}

// Rules whose selectors reach out from a base selector — `rdio-status .led::after` reaches
// from `rdio-status .led`. The resting cascade only: a media block's rules answer the
// condition, not the picture.
function reaching(base, test) {
  return RULES().filter((rule) =>
    rule.media === null
    && rule.selector.split(',').some((p) => flat(p).startsWith(flat(base)) && test(flat(p), rule.declarations)));
}

const LIT_GLOW = (color) => `0 0 6px 3px ${color}`;

describe('the status light — the plain v6.6.3 light', () => {
  it('draws nothing on the light\'s own face', () => {
    const glyphed = reaching('rdio-status .led', (selector) => selector.includes('::after'));

    assert.deepEqual(glyphed.map((rule) => rule.selector), [],
      'the stylesheet draws on the light\'s face; the light is the face, the glow, and the blink');

    const fonts = reaching('rdio-status .led', (selector, declarations) => 'font-family' in declarations);

    assert.deepEqual(fonts.map((rule) => rule.selector), [],
      'the light carries an icon font; its face has no glyph to set');
  });

  it('rests as the dark face it rests on', () => {
    expect('rdio-status .led', 'background', 'rgb(80, 80, 80)');
    expect('rdio-status .led', 'height', '12px');
    expect('rdio-status .led', 'width', '24px');
    expect('rdio-status .led', 'margin-left', '24px');
  });

  it('lights with the green face and its glow', () => {
    expect('rdio-status .led.on', 'background', 'rgb(0, 230, 118)');
    expect('rdio-status .led.on', 'box-shadow', LIT_GLOW('rgb(0, 230, 118)'));
  });

  it('lights in the eight colours the talkgroups carry', () => {
    const colors = {
      blue: 'rgb(41, 121, 255)',
      cyan: 'rgb(0, 229, 255)',
      green: 'rgb(0, 230, 118)',
      magenta: 'rgb(213, 0, 249)',
      orange: 'rgb(255, 145, 0)',
      red: 'rgb(255, 23, 68)',
      white: 'rgb(255, 255, 255)',
      yellow: 'rgb(255, 234, 0)',
    };

    for (const [name, color] of Object.entries(colors)) {
      expect(`rdio-status .led.on.${name}`, 'background', color);
      expect(`rdio-status .led.on.${name}`, 'box-shadow', LIT_GLOW(color));
    }
  });

  it('blinks the paused light on the two-second cycle', () => {
    expect('rdio-status .led.paused', 'animation', 'led-blink 2s step-end infinite');

    const stops = keyframes(SHEET(), 'led-blink');

    assert.ok(stops, 'the stylesheet has no led-blink cycle to blink on');
    assert.equal(stops['50%']?.background && flat(stops['50%'].background), 'rgb(80,80,80)',
      'the blink\'s dark half does not land on the resting face');
    assert.equal(stops['50%']?.['box-shadow'] && flat(stops['50%']['box-shadow']), 'none',
      'the blink\'s dark half keeps a glow; the dark half is unlit');
  });
});

describe('the reduced state — the paused light keeps a mark of its own', () => {
  const REDUCED = '(prefers-reduced-motion:reduce)';
  const FORCED = '(forced-colors:active)';

  it('keeps the blink guard that quiets the paused light', () => {
    expect('rdio-status .led.paused', 'animation', 'none', REDUCED);
  });

  it('gives the quieted paused light a ring the live face does not wear', () => {
    expect('rdio-status .led.paused', 'box-shadow', '0 0 0 2px rgb(20, 20, 20)', REDUCED);
  });

  it('carries the ring in the system ink when the system takes the colours', () => {
    expect('rdio-status .led.paused', 'box-shadow', '0 0 0 2px CanvasText', FORCED);
  });
});

describe('the display rows — the v6.6.3 space-between placement', () => {
  it('places each row\'s children at the ends it places them at', () => {
    expect('rdio-display .row', 'justify-content', 'space-between');
  });

  it('gives no row child a third of the row to stand in', () => {
    const gridded = RULES().filter((rule) =>
      rule.media === null
      && flat(rule.selector).startsWith(flat('rdio-display .row'))
      && Object.entries(rule.declarations).some(([prop, val]) => prop === 'flex' && flat(val) === '33%'));

    assert.deepEqual(gridded.map((rule) => rule.selector), [],
      'the rows run the three-column grid; the v6.6.3 row places its children with space-between');
  });

  it('keeps the big, right, and small rows the v6.6.3 rows are', () => {
    expect('rdio-display .row.big', 'font-size', '24px');
    expect('rdio-display .row.big', 'height', '32px');
    expect('rdio-display .row.big', 'line-height', '32px');
    expect('rdio-display .row.right', 'justify-content', 'flex-end');
    expect('rdio-display .row.small', 'font-size', '12px');
    expect('rdio-display .row.small', 'height', '14px');
    expect('rdio-display .row.small', 'line-height', '14px');
  });
});

describe('the control buttons — the v6.6.3 bevel, dip, and dots', () => {
  const base = 'rdio-controls .rdio-button';

  it('bevels the button the way the bevel bevels it', () => {
    expect(base, 'background', 'var(--def)');
    expect(base, 'border-style', 'solid');
    expect(base, 'border-width', '1px');
    expect(base, 'border-bottom-color', 'rgba(0, 0, 0, 0.87)');
    expect(base, 'border-left-color', 'rgba(255, 255, 255, 0.7)');
    expect(base, 'border-right-color', 'rgba(0, 0, 0, 0.87)');
    expect(base, 'border-top-color', 'rgba(255, 255, 255, 0.7)');
    expect(base, 'font-weight', '500');
    expect(base, 'height', '40px');
    expect(base, 'line-height', '18px');
    expect(base, 'margin', '2px');
    expect(base, 'padding', '2px 8px');
    expect(base, 'position', 'relative');
    expect(base, 'overflow', 'hidden');
    expect(base, 'text-shadow', '0 0 4px rgb(0, 0, 0)');
  });

  it('says the button\'s words in the v6.6.3 type', () => {
    expect(base, 'color', 'rgb(250, 250, 250)');
    expect(base, 'font-size', '12px');
    expect(base, 'min-width', '80px');
    expect(base, 'text-transform', 'uppercase');
  });

  it('dips the pressed button and lifts it back', () => {
    expect('rdio-controls .rdio-button:active', 'top', '2px');
    expect('rdio-controls .rdio-button:active', 'transform', 'scale(0.98)');
    expect('rdio-controls .rdio-button:active', 'transform-origin', 'bottom center');
  });

  it('sits the status dot in the corner it sits in', () => {
    for (const state of ['off', 'on', 'partial']) {
      const dot = `rdio-controls .rdio-button.${state}::after`;

      assert.ok(has(dot), `the ${state} button has no status dot`);
      expect(dot, 'height', '6px');
      expect(dot, 'width', '6px');
      expect(dot, 'top', '4px');
      expect(dot, 'right', '4px');
      expect(dot, 'animation', 'blink 1s linear infinite both');
    }

    expect('rdio-controls .rdio-button.off::after', 'background', 'var(--red)');
    expect('rdio-controls .rdio-button.on::after', 'background', 'var(--green)');
    expect('rdio-controls .rdio-button.partial::after', 'background', 'var(--yellow)');
  });
});
