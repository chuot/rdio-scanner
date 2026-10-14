// The name keeps its text. The asterisk is a mark the stylesheet draws beside a field's name; it is
// never a character the webapp puts into the name, so a screen reader, a search, and the label a
// control is attached to all read the same word the operator reads.
//
// The readings take the marked names on every shape the webapp names a field with — the label inside a
// form field, the caption span a hand-built row names its field with, and the label of a panel's own
// form — and read the text of each marked element. The stylesheet reading counts the rules that draw
// the mark. The last two readings are pins: no name on these surfaces carries an asterisk as text, and
// no source under the presentation directories composes one into a name.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-systems.js';
import '../presentation/admin/rdio-admin-sites.js';
import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-units.js';
import '../presentation/admin/rdio-admin-groups.js';
import '../presentation/admin/rdio-admin-tags.js';
import '../presentation/admin/rdio-admin-options.js';
import '../presentation/admin/rdio-admin-tools.js';

import { connectFields, findAll, requiredMark, texts } from './dom-shim.js';

const METRO = { id: 1, label: 'Metro', order: 2, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };
const COUNTY = { id: 2, label: 'County', order: 1, systemRef: 2, blacklists: '', sites: [], talkgroups: [], units: [] };

const SITES = { 1: [], 2: [{ id: 4, label: 'East', siteRef: 40, order: 1 }] };
const TALKGROUPS = { 1: [], 2: [{ id: 7, label: 'Fire', name: 'Fire dispatch', talkgroupRef: 70, order: 1, groupIds: [1], tagId: 3, alert: '', delay: null, frequency: null, led: '', type: '' }] };
const UNITS = { 1: [], 2: [{ id: 9, label: 'Car', unitRef: 100, unitFrom: 100, unitTo: 199, order: 1 }] };

const GROUPS = [{ id: 1, label: 'Dispatch', alert: '', led: '' }];
const TAGS = [{ id: 3, label: 'Fire', alert: '', led: '' }];

const OPTIONS = {
  audioConversion: 'opus',
  autoPopulate: false,
  branding: 'Ridio',
  disableDuplicateDetection: 0,
  email: 'ops@example.test',
  keypadBeeps: 'standard',
  maxClients: 5,
  pruneDays: 7,
  showListenersCount: 0,
  sortTalkgroups: false,
};

function slice(working) {
  return { baseline: structuredClone(working), working: structuredClone(working), dirty: false, invalid: false, conflict: false };
}

function seed(store) {
  const state = { auth: { authenticated: true, docker: false, passwordNeedChange: false }, alerts: { fire: {} } };

  for (const [level, bySystem] of [['sites', SITES], ['talkgroups', TALKGROUPS], ['units', UNITS]]) {
    for (const system of [COUNTY, METRO]) {
      state[`subsection:${level}:${system.id}`] = slice(bySystem[system.id]);
    }
  }

  store.setState({
    ...state,
    'section:systems': slice([METRO, COUNTY]),
    'section:groups': slice(GROUPS),
    'section:tags': slice(TAGS),
    'section:options': slice(OPTIONS),
  });

  return store;
}

function attach(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

function addRow(el) {
  findAll(el, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes('add'))[0].click();
  connectFields(el);

  // The systems tree opens the row it adds as a `details` element; the row sections are divs.
  return findAll(el, (node) => node.tag === 'details')[0]
    ?? findAll(el, (node) => node.tag === 'div' && node.className?.split(/\s+/).includes('rdio-admin-row'))[0];
}

function panels(el) {
  return findAll(el, (node) => node.className?.split(/\s+/).includes('rdio-admin-row'));
}

// The caption span a row names its field with: the systems tree writes it as `.body`, the hand-built
// sections as `.field-title`. A label inside a form field is the other shape, and is read by its tag.
function spanClass(node) {
  return node.className?.split(/\s+/).find((name) => name === 'field-title' || name === 'body');
}

// The elements that name the fields of a row, panel, or section: the label inside each form field, and
// the caption spans. A row built by the shared row builders names each field twice, so a name counts
// once and the element that counts is the first one the page shows.
function namedElements(root) {
  const found = [];

  for (const node of findAll(root, (child) => child.tag === 'rdio-form-field')) {
    const label = findAll(node, (child) => child.tag === 'label')[0];

    if (label) found.push(label);
  }

  for (const span of findAll(root, (child) => spanClass(child))) found.push(span);

  return found;
}

function markedElements(root) {
  const seen = new Set();

  return namedElements(root).filter((el) => requiredMark(el) && !seen.has(el.textContent) && seen.add(el.textContent));
}

function markedNames(root) {
  return markedElements(root).map((el) => el.textContent).sort();
}

// ---------------------------------------------------------------------------
// The marked names carry their own text, on every shape the webapp names a field with.
// ---------------------------------------------------------------------------

const MARKED_SHAPES = [
  ['systems row', () => addRow(attach('rdio-admin-systems')), ['Id', 'Label']],
  ['sites row', () => addRow(attach('rdio-admin-sites')), ['Id', 'Label']],
  ['talkgroups row', () => addRow(attach('rdio-admin-talkgroups')), ['Groups', 'Id', 'Label', 'Name', 'Tag']],
  ['units row', () => addRow(attach('rdio-admin-units')), ['Label']],
  ['groups row', () => addRow(attach('rdio-admin-groups')), ['Label']],
  ['tags row', () => addRow(attach('rdio-admin-tags')), ['Label']],
  ['options panel', () => attach('rdio-admin-options'), ['Keypad Beep Style', 'Max Clients', 'Prune Days']],
  [
    'change password panel',
    () => panels(attach('rdio-admin-tools')).find((panel) => texts(findAll(panel, (child) => child.className?.split(/\s+/).includes('row-title')))[0] === 'Change password'),
    ['Current password', 'New password', 'Verify new password'],
  ],
];

for (const [name, surface, expected] of MARKED_SHAPES) {
  test(`the marked names of the ${name} are the field's own words`, () => {
    seed(getAdminContext().store);

    const root = surface();

    assert.deepEqual(markedNames(root), expected, 'the names carrying the mark are the names the section insists on');

    for (const el of markedElements(root)) {
      assert.ok(!el.textContent.includes('*'), `the marked name ${el.textContent} carries no asterisk in its text`);
      assert.equal(el.textContent.trim(), el.textContent, `the marked name ${el.textContent} is the name alone, with no mark around it`);
    }
  });
}

// ---------------------------------------------------------------------------
// The stylesheet draws the mark: one rule for the config sections, and the OpenAPI box's own.
// ---------------------------------------------------------------------------

const stylesDir = join(dirname(fileURLToPath(import.meta.url)), '..', 'styles');
const presentationDirs = [
  join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..', 'admin', 'frontend', 'presentation'),
  join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..', 'common', 'frontend', 'presentation'),
];

function cssRules(stylesheet) {
  const text = stylesheet.replace(/\/\*[\s\S]*?\*\//g, '');

  return [...text.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(([, selector, block]) => ({
    selector: selector.trim().replace(/\s+/g, ' '),
    block: block.trim(),
  }));
}

function sourceFiles(dir) {
  return readdirSync(dir, { recursive: true })
    .filter((entry) => entry.endsWith('.js'))
    .map((entry) => join(dir, entry));
}

test('the mark is drawn by one rule for the config sections, beside the OpenAPI box\'s own', () => {
  const rules = cssRules(readFileSync(join(stylesDir, 'admin.css'), 'utf8'));

  const marker = rules.filter((rule) => rule.selector.includes('[data-required]') && rule.block.includes('content') && rule.block.includes('*'));

  const openApi = marker.filter((rule) => rule.selector.includes('rdio-admin-openapi-box'));
  const sections = marker.filter((rule) => !rule.selector.includes('rdio-admin-openapi-box'));

  assert.equal(openApi.length, 1, 'the OpenAPI box keeps the one rule it already had, and the webapp adds no second one');
  assert.equal(sections.length, 1, 'the config sections draw the mark from exactly one rule');

  const [rule] = sections;

  assert.match(rule.block, /content:\s*" \*"/, 'the mark is the asterisk with the space before it, the way Material composed it');
  assert.match(rule.block, /color:\s*var\(--rdio-warn\)/, 'the mark takes the warning colour the theme already carries');

  const selectors = rule.selector.split(',').map((part) => part.trim());

  for (const shape of ['rdio-form-field label', '.field-title', '.body']) {
    assert.ok(
      selectors.some((part) => part.startsWith('rdio-admin ') && part.endsWith(`${shape}[data-required]::after`)),
      `the rule draws the mark on the name a section names its field with as ${shape}, inside the admin page`,
    );
  }
});

// ---------------------------------------------------------------------------
// The pins: nothing on these surfaces, and nothing in the sources, puts the asterisk into a name.
// ---------------------------------------------------------------------------

test('no field name on these surfaces carries an asterisk as text', () => {
  seed(getAdminContext().store);

  const surfaces = [
    addRow(attach('rdio-admin-systems')),
    addRow(attach('rdio-admin-sites')),
    addRow(attach('rdio-admin-talkgroups')),
    addRow(attach('rdio-admin-units')),
    addRow(attach('rdio-admin-groups')),
    addRow(attach('rdio-admin-tags')),
    attach('rdio-admin-options'),
    panels(attach('rdio-admin-tools')).find((panel) => texts(findAll(panel, (child) => child.className?.split(/\s+/).includes('row-title')))[0] === 'Change password'),
  ];

  for (const root of surfaces) {
    for (const el of namedElements(root)) {
      assert.ok(!el.textContent.includes('*'), `${el.textContent} is the name the operator reads, mark and all`);
    }
  }
});

test('no source under the presentation directories composes an asterisk into a name', () => {
  // The mark Material composed was a space and an asterisk. A name built with one of those inside its
  // own text would reach the operator as part of the word. The value the systems lists use for every
  // system at once is a bare asterisk, and is not a name.
  for (const dir of presentationDirs) {
    for (const file of sourceFiles(dir)) {
      const source = readFileSync(file, 'utf8');

      assert.ok(
        !/['"`][^'"`\n]*( \*|\* )[^'"`\n]*['"`]/.test(source),
        `${relative(process.cwd(), file)} carries no name composed with an asterisk`,
      );
    }
  }
});
