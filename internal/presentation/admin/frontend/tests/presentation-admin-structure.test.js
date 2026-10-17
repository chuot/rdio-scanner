// The structure the operator's keyboard and screen reader travel through, in the administration
// application: the skip link that comes before every control of the page, the content region the
// section sits in, the heading that names the section the operator is on, and the list the sections
// themselves sit in.
//
// The skip link is the page's own markup and the stylesheet's own rule, so those two readings are
// taken from the files the build ships. The rest is taken from the rendered shell: the nav, the
// content region, and the heading the shell puts at its head, read the way an operator reads them.
//
// The fifteen section labels are written out here on purpose. They are the words the operator
// reads in the navigation; a test that takes them from the code under test would pass whatever the
// code decides to call a section.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin.js';

import { findAll } from './dom-shim.js';

const presentationDir = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..');

const INDEX_HTML = join(presentationDir, 'admin', 'frontend', 'index.html');
const STYLESHEET = join(presentationDir, 'admin', 'frontend', 'styles', 'admin.css');

const SECTIONS = [
  { name: 'access', label: 'Access' },
  { name: 'apikeys', label: 'API Keys' },
  { name: 'dashboard', label: 'Dashboard' },
  { name: 'dirwatch', label: 'Dirwatch' },
  { name: 'downstreams', label: 'Downstreams' },
  { name: 'groups', label: 'Groups' },
  { name: 'logs', label: 'Logs' },
  { name: 'openapi', label: 'OpenAPI' },
  { name: 'options', label: 'Options' },
  { name: 'sites', label: 'Sites' },
  { name: 'systems', label: 'Systems' },
  { name: 'tags', label: 'Tags' },
  { name: 'talkgroups', label: 'Talkgroups' },
  { name: 'tools', label: 'Tools' },
  { name: 'units', label: 'Units' },
];

const OPTIONS = {
  branding: 'Harness Dispatch',
  email: 'harness@example.com',
  maxClients: 200,
  dimmerDelay: 5000,
  pruneDays: 7,
  autoPopulate: false,
  playbackGoesLive: true,
  showListenersCount: true,
  sortTalkgroups: false,
  time12hFormat: true,
  keypadBeeps: 'uniden',
  audioConversion: 0,
  duplicateDetectionTimeFrame: 500,
  disableDuplicateDetection: true,
};

function slice(value) {
  return { baseline: structuredClone(value), working: structuredClone(value), dirty: false, invalid: false, conflict: false };
}

// The shell, on the section it starts on. Every section the operator can reach is seeded with the
// empty list the server answers with when nothing is defined, so the reading is taken on a page the
// operator can actually be on.
function shell() {
  getAdminContext().store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    todos: [],
    'section:access': slice([]),
    'section:apikeys': slice([]),
    'section:dirwatch': slice([]),
    'section:downstreams': slice([]),
    'section:groups': slice([]),
    'section:options': slice(OPTIONS),
    'section:systems': slice([]),
    'section:tags': slice([]),
  });

  const el = new (customElements.get('rdio-admin'))();

  el.connectedCallback();

  return el;
}

// The region the section page is painted in.
function contentRegion(el) {
  return findAll(el, (node) => node.tag === 'section' && node.className?.includes('rdio-admin-content'))[0] ?? null;
}

function headings(root) {
  return findAll(root, (node) => /^h[1-6]$/.test(node.tag));
}

function navLists(el) {
  const nav = findAll(el, (node) => node.tag === 'nav')[0] ?? null;

  return { nav, list: nav ? findAll(nav, (node) => node.tag === 'ul' || node.tag === 'ol')[0] ?? null : null };
}

function navButtons(nav) {
  return findAll(nav, (node) => node.tag === 'button' && node.dataset.section !== undefined);
}

// The body's own children, in the order the page ships them: the skip link has to be the first of
// them, because the first thing a keyboard finds on a page is the first thing the page puts there.
function bodyElements(html) {
  const body = html.slice(html.indexOf('<body') + 5, html.indexOf('</body>'));

  return [...body.matchAll(/<([a-zA-Z][\w-]*)\b([^>]*)>/g)].map((m) => ({ tag: m[1], attributes: m[2] }));
}

describe('admin structure — the route into the page', () => {
  it('opens with a skip link before every control the page owns', () => {
    const first = bodyElements(readFileSync(INDEX_HTML, 'utf8'))[0];

    assert.equal(first?.tag, 'a', `the page's first element is <${first?.tag}>, not a link the keyboard reaches first`);
    assert.match(first.attributes, /class="[^"]*\bskip\b/, 'the first link is not the skip link the guide uses');

    assert.ok(first.attributes.match(/href="#([^"]+)"/), 'the skip link does not point at a place on the page');
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

  it('lands in the content region the section is painted in', () => {
    const first = bodyElements(readFileSync(INDEX_HTML, 'utf8'))[0];
    const target = first.attributes.match(/href="#([^"]+)"/)[1];

    const el = shell();
    const region = contentRegion(el);

    assert.ok(region, 'the section page is not painted in a content region');
    assert.equal(region.getAttribute('id'), target, `the skip link points at #${target}, and the content region is #${region.getAttribute('id') ?? 'nothing'}`);
  });
});

describe('admin structure — the section the operator is on', () => {
  it('names the content region by the heading of the section on the page', () => {
    const el = shell();
    const region = contentRegion(el);

    assert.ok(region, 'the section page is not painted in a content region');

    const headingId = region.getAttribute('aria-labelledby');

    assert.ok(headingId, 'the content region is not named by anything');

    const heading = headings(el).find((node) => node.getAttribute('id') === headingId);

    assert.ok(heading, `the content region names ${headingId}, which is not a heading on the page`);
    assert.equal(heading.textContent.trim(), 'Dashboard', 'the heading does not name the section the operator lands on');
  });

  it('names every section it can put on the page', () => {
    const el = shell();

    for (const { name, label } of SECTIONS) {
      el.openSection(name);

      const region = contentRegion(el);
      const heading = headings(el).find((node) => node.getAttribute('id') === region?.getAttribute('aria-labelledby'));

      assert.ok(heading, `the ${name} section page carries no heading naming it`);
      assert.equal(heading.textContent.trim(), label, `the heading on the ${name} section page says "${heading.textContent.trim()}"`);
    }
  });

  it('moves the heading and the region name together when the operator changes section', () => {
    const el = shell();

    el.openSection('systems');

    const region = contentRegion(el);
    const heading = headings(el).find((node) => node.getAttribute('id') === region?.getAttribute('aria-labelledby'));

    assert.ok(heading, 'the systems section page carries no heading naming it');
    assert.equal(heading.textContent.trim(), 'Systems');

    // The heading is the region's name, so a screen reader reading the region says the section the
    // operator is on, and says it again when the operator moves.
    assert.equal(region.getAttribute('aria-labelledby'), heading.getAttribute('id'));

    el.openSection('tags');

    const moved = headings(el).find((node) => node.getAttribute('id') === contentRegion(el).getAttribute('aria-labelledby'));

    assert.equal(moved?.textContent.trim(), 'Tags', 'the region kept its old name after the operator changed section');
  });

  it('keeps the heading levels in order, with one app heading above the section heading', () => {
    const el = shell();
    const levels = headings(el).map((node) => Number(node.tag.slice(1)));

    assert.equal(levels.filter((level) => level === 1).length, 1, 'the page does not carry exactly one app heading');
    assert.ok(levels.includes(2), 'the section heading is not one level below the app heading');
    assert.ok(!levels.some((level, i) => i > 0 && level - levels[i - 1] > 1), `the heading levels skip: ${levels.join(', ')}`);
  });
});

describe('admin structure — the list of sections', () => {
  it('puts the section items in a list a screen reader can count', () => {
    const el = shell();
    const { nav, list } = navLists(el);

    assert.ok(nav, 'the sections are not in a navigation landmark');
    assert.ok(list, `the ${navButtons(nav).length} section items are bare children of the navigation, not items of a list`);

    const items = findAll(list, (node) => node.tag === 'li');

    assert.equal(items.length, SECTIONS.length, 'the list does not hold one item per section');

    for (const button of navButtons(nav)) {
      assert.equal(button.parent.tag, 'li', `the ${button.dataset.section} item is not inside a list item`);
    }
  });

  it('marks only the section the operator is on as current', () => {
    const el = shell();

    for (const { name } of SECTIONS) {
      el.openSection(name);

      const currents = navButtons(findAll(el, (node) => node.tag === 'nav')[0]).filter((b) => b.getAttribute('aria-current') !== null);
      const pages = currents.filter((b) => b.getAttribute('aria-current') === 'page');
      const noise = currents.filter((b) => b.getAttribute('aria-current') !== 'page');

      assert.equal(pages.length, 1, `the ${name} section page marks ${pages.length} items as current`);
      assert.equal(noise.length, 0, `the ${name} section page leaves ${noise.length} items announcing aria-current="${noise[0]?.getAttribute('aria-current')}"`);
      assert.equal(pages[0].dataset.section, name, 'the item marked current is not the section on the page');
    }
  });
});
