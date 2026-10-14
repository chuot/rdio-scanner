// The structure the listener's keyboard and screen reader travel through: the skip link that comes
// before every control of the page, the region the listener's own content sits in, the heading that
// names the application, and the headings that name the two panels the listener opens.
//
// The skip link is the page's own markup and the stylesheet's own rule, so those readings are taken
// from the files the build ships. The rest is taken from the rendered shell, read the way a screen
// reader reads a page: the landmarks it announces, the headings it can list, and the name each
// region carries.
//
// The application name and the two panel names are written out here on purpose. They are the words
// the listener reads on the screen; a test that took them from the code under test would pass
// whatever the code decided to call them.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { getAppContext } from '../application/context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/listener/rdio-app.js';
import '../presentation/listener/rdio-controls.js';
import '../presentation/listener/rdio-display.js';
import '../presentation/listener/rdio-search-panel.js';
import '../presentation/listener/rdio-select-panel.js';
import '../presentation/listener/rdio-status.js';

import { findAll } from './dom-shim.js';

const presentationDir = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..');

const INDEX_HTML = join(presentationDir, 'scanner', 'frontend', 'index.html');
const STYLESHEET = join(presentationDir, 'scanner', 'frontend', 'styles', 'rdio.css');

const APP_NAME = 'Rdio Scanner';
const PANEL_NAMES = { search: 'Search', select: 'Select' };

// The body's own children, in the order the page ships them: the first thing a keyboard finds on a
// page is the first thing the page puts there.
function bodyElements(html) {
  const body = html.slice(html.indexOf('<body') + 5, html.indexOf('</body>'));

  return [...body.matchAll(/<([a-zA-Z][\w-]*)\b([^>]*)>/g)].map((m) => ({ tag: m[1], attributes: m[2] }));
}

function skipLinkTarget(html) {
  return bodyElements(html)[0]?.attributes.match(/href="#([^"]+)"/)?.[1] ?? null;
}

// The shell, rendered the way the page renders it. The promo the shell schedules is a ten second
// timer the browser owns; the shim does not model the clock, so the timer is handed to the test
// during the render and left unrun, and the real clock is put back before the reading is taken.
function shell() {
  const el = document.createElement('rdio-app');

  const realSetTimeout = globalThis.setTimeout;
  const scheduled = [];

  globalThis.setTimeout = (fn, ms) => {
    scheduled.push([fn, ms]);

    return scheduled.length;
  };

  try {
    el.connectedCallback();
  } finally {
    globalThis.setTimeout = realSetTimeout;
  }

  el.scheduled = scheduled;

  return el;
}

function headings(root) {
  return findAll(root, (node) => /^h[1-6]$/.test(node.tag));
}

function mainRegion(el) {
  return findAll(el, (node) => node.tag === 'main')[0] ?? null;
}

// The name a screen reader says for a region: the label it carries, or the text of the heading it
// is labelled by.
function regionName(el, region) {
  const label = region.getAttribute('aria-label');

  if (label) return label.trim();

  const ids = (region.getAttribute('aria-labelledby') ?? '').split(/\s+/).filter(Boolean);

  return findAll(el, (node) => ids.includes(node.getAttribute('id'))).map((node) => node.textContent.trim()).join(' ');
}

describe('listener structure — the route into the page', () => {
  it('opens with a skip link before every control the page owns', () => {
    const first = bodyElements(readFileSync(INDEX_HTML, 'utf8'))[0];

    assert.equal(first?.tag, 'a', `the page's first element is <${first?.tag}>, not a link the keyboard reaches first`);
    assert.match(first.attributes, /class="[^"]*\bskip\b/, 'the first link is not the skip link the guide uses');
    assert.ok(first.attributes.match(/href="#[^"]+"/), 'the skip link does not point at a place on the page');
  });

  it('brings the skip link on screen when the keyboard reaches it', () => {
    const css = readFileSync(STYLESHEET, 'utf8');

    const base = css.match(/(^|\n)\.skip\s*\{([^}]*)\}/);
    const focus = css.match(/\.skip:focus\b[^{]*\{([^}]*)\}/);

    assert.ok(base, 'the stylesheet has no resting rule for the skip link');
    assert.ok(focus, 'the stylesheet has no rule that reveals the skip link on focus');

    // Parked by clipping itself, and revealed by taking the clip off. A link moved outside the
    // page's scrollable area is dropped from the keyboard's route — the browser will not land on a
    // control it cannot scroll into view — and this is the control the keyboard has to reach first.
    assert.match(base[2], /clip-path:\s*inset\(\s*50%\s*\)/,
      'the skip link is not parked out of sight at rest, so it is on the page before the operator asks for it');
    assert.match(focus[1], /clip-path:\s*none/, 'the rule that reveals the skip link does not bring it back on screen');
  });

  it('lands in the region the listener works in', () => {
    const target = skipLinkTarget(readFileSync(INDEX_HTML, 'utf8'));

    const el = shell();
    const main = mainRegion(el);

    assert.ok(main, 'the listener works outside a main region');
    assert.equal(main.getAttribute('id'), target, `the skip link points at #${target}, and the content region is #${main.getAttribute('id') ?? 'nothing'}`);
  });
});

describe('listener structure — the region and its name', () => {
  it('names the main region by the application', () => {
    const el = shell();
    const main = mainRegion(el);

    assert.ok(main, 'the listener works outside a main region');
    assert.match(regionName(el, main), new RegExp(APP_NAME, 'i'), `the main region is named "${regionName(el, main)}"`);
  });

  it('keeps the content the listener reads inside the named region', () => {
    const el = shell();
    const main = mainRegion(el);

    assert.ok(main, 'the listener works outside a main region');

    for (const tag of ['rdio-status', 'rdio-display', 'rdio-controls']) {
      const surface = findAll(main, (node) => node.tag === tag)[0];

      assert.ok(surface, `the ${tag} the listener reads is outside the main region`);
    }
  });

  it('carries one heading naming the application', () => {
    const el = shell();
    const named = headings(el).filter((node) => new RegExp(APP_NAME, 'i').test(node.textContent));

    assert.equal(named.length, 1, `the page carries ${headings(el).length} headings and ${named.length} name the application`);
    assert.equal(named[0].tag, 'h1', `the heading naming the application is a <${named[0].tag}>, not the page heading`);
  });

  it('keeps the heading levels in order, with the panel headings below the app heading', () => {
    const el = shell();
    const levels = headings(el).map((node) => Number(node.tag.slice(1)));

    assert.equal(levels.filter((level) => level === 1).length, 1, 'the page does not carry exactly one app heading');
    assert.ok(!levels.some((level, i) => i > 0 && level - levels[i - 1] > 1), `the heading levels skip: ${levels.join(', ')}`);
  });
});

describe('listener structure — the panels the listener opens', () => {
  for (const [name, label] of Object.entries(PANEL_NAMES)) {
    it(`names the ${name} panel with a heading`, () => {
      const el = shell();
      const panel = findAll(el, (node) => node.tag === 'aside' && node.className.split(/\s+/).includes('panel') && node.className.split(/\s+/).includes(name))[0];

      assert.ok(panel, `the ${name} panel is not on the page`);

      const heading = headings(panel)[0];

      assert.ok(heading, `the ${name} panel opens with no heading in it`);
      assert.equal(heading.textContent.trim(), label, `the ${name} panel's heading says "${heading.textContent.trim()}"`);
    });
  }
});
