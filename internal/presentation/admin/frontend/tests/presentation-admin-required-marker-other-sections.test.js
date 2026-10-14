// The seven administration sections outside the systems tree, and the Tools → Change password panel,
// read for the asterisk on the name of every field the webapp insists on.
//
// These sections build their rows by hand, and they name a field in two shapes: a <label> inside the
// form field (Access, API keys, Downstreams, Dirwatch, the password panel), or a caption span beside
// it (Groups, Tags, Options). The mark rides whichever node names the field.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-groups.js';
import '../presentation/admin/rdio-admin-tags.js';
import '../presentation/admin/rdio-admin-access.js';
import '../presentation/admin/rdio-admin-apikeys.js';
import '../presentation/admin/rdio-admin-downstreams.js';
import '../presentation/admin/rdio-admin-dirwatch.js';

import { connectFields, findAll, requiredMark, texts } from './dom-shim.js';

const METRO = { id: 1, label: 'Metro', order: 2, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };
const COUNTY = { id: 2, label: 'County', order: 1, systemRef: 2, blacklists: '', sites: [], talkgroups: [], units: [] };

const GROUPS = [{ id: 1, label: 'Dispatch', alert: '', led: '' }];
const TAGS = [{ id: 3, label: 'Fire', alert: '', led: '' }];

const ACCESS = [{ id: 1, order: 1, code: 'alpha', ident: 'AAA', systems: '*' }];
const APIKEYS = [{ id: 1, order: 1, key: 'first-key', ident: 'K1', systems: '*', disabled: false }];
const DOWNSTREAMS = [{ id: 1, order: 1, url: 'https://first.example', apikey: 'dk-1', systems: '*', disabled: false }];
const DIRWATCH = [{ id: 1, order: 1, directory: '/var/spool/first', extension: 'mp3', mask: '#SYS #TG', type: 'p25', systemId: 2, siteId: null, talkgroupId: 7, frequency: 1, delay: 2000, disabled: false }];

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

function seed(store, extra = {}) {
  store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    alerts: { fire: {} },
    'section:systems': slice([METRO, COUNTY]),
    'section:groups': slice(GROUPS),
    'section:tags': slice(TAGS),
    'section:access': slice(ACCESS),
    'section:apikeys': slice(APIKEYS),
    'section:downstreams': slice(DOWNSTREAMS),
    'section:dirwatch': slice(DIRWATCH),
    'section:options': slice(OPTIONS),
    ...extra,
  });

  return store;
}

function attach(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

function control(el, className) {
  return findAll(el, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes(className))[0];
}

function rows(el) {
  return findAll(el, (node) => node.tag === 'div' && node.className?.split(/\s+/).includes('rdio-admin-row'));
}

function addRow(el) {
  control(el, 'add').click();
  connectFields(el);

  return rows(el)[0];
}

// The names a row carries the asterisk on: the label inside a form field, and the caption span a
// hand-built row names its field with. A systems-tree style row names its field in both shapes at
// once, so a name counts once.
function markedNames(row) {
  const names = new Set();

  for (const node of findAll(row, (child) => child.tag === 'rdio-form-field')) {
    const label = findAll(node, (child) => child.tag === 'label')[0];

    if (label && requiredMark(label)) names.add(label.textContent);
  }

  for (const span of findAll(row, (child) => ['field-title', 'body'].includes(spanClass(child)))) {
    if (requiredMark(span)) names.add(span.textContent);
  }

  return [...names].sort();
}

function spanClass(node) {
  return node.className?.split(/\s+/).find((name) => name === 'field-title' || name === 'body');
}

// ---------------------------------------------------------------------------
// The rows the operator added, read before they reach anything.
// ---------------------------------------------------------------------------

const MARKED_ROWS = [
  ['groups', 'rdio-admin-groups', ['Label']],
  ['tags', 'rdio-admin-tags', ['Label']],
  ['access', 'rdio-admin-access', ['Code', 'Ident']],
  ['API keys', 'rdio-admin-apikeys', ['Ident', 'Key']],
  ['downstreams', 'rdio-admin-downstreams', ['API key', 'URL']],
  ['dirwatch', 'rdio-admin-dirwatch', ['Directory']],
];

for (const [name, tag, marked] of MARKED_ROWS) {
  test(`a new ${name} row names the fields the webapp insists on`, () => {
    seed(getAdminContext().store);

    assert.deepEqual(markedNames(addRow(attach(tag))), marked, `the new ${name} row carries the mark on its name and nowhere else`);
  });
}

// ---------------------------------------------------------------------------
// A saved row carries the same marks as a new one.
// ---------------------------------------------------------------------------

test('a saved access row carries the same marks as the row the operator added', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-access');
  const added = addRow(el);
  const saved = rows(el).find((row) => row !== added);

  assert.deepEqual(markedNames(saved), ['Code', 'Ident'], 'the marks answer the fields, not the values in them');
});
