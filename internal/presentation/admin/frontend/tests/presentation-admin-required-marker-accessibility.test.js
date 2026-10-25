// aria-required puts the required mark on the control a screen reader announces:
// the input box, or the select the pick-list primitive builds

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-systems.js';
import '../presentation/admin/rdio-admin-sites.js';
import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-units.js';
import '../presentation/admin/rdio-admin-groups.js';
import '../presentation/admin/rdio-admin-tags.js';
import '../presentation/admin/rdio-admin-access.js';
import '../presentation/admin/rdio-admin-apikeys.js';
import '../presentation/admin/rdio-admin-downstreams.js';
import '../presentation/admin/rdio-admin-dirwatch.js';
import '../presentation/admin/rdio-admin-options.js';
import '../presentation/admin/rdio-admin-tools.js';

import { connectFields, findAll, requiredMark } from './dom-shim.js';

const METRO = { id: 1, label: 'Metro', order: 2, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };
const COUNTY = { id: 2, label: 'County', order: 1, systemRef: 2, blacklists: '', sites: [], talkgroups: [], units: [] };

const SITES = { 1: [], 2: [{ id: 4, label: 'East', siteRef: 40, order: 1 }] };
const TALKGROUPS = { 1: [], 2: [{ id: 7, label: 'Fire', name: 'Fire dispatch', talkgroupRef: 70, order: 1, groupIds: [1], tagId: 3, alert: '', delay: null, frequency: null, led: '', type: '' }] };
const UNITS = { 1: [], 2: [{ id: 9, label: 'Car', unitRef: 100, unitFrom: 100, unitTo: 199, order: 1 }] };

const ACCESS = [{ id: 1, order: 1, code: 'alpha', ident: 'AAA', systems: '*' }];
const APIKEYS = [{ id: 1, order: 1, key: 'first-key', ident: 'K1', systems: '*', disabled: false }];
const DOWNSTREAMS = [{ id: 1, order: 1, url: 'https://first.example', apikey: 'dk-1', systems: '*', disabled: false }];

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
    'section:groups': slice([{ id: 1, label: 'Dispatch', alert: '', led: '' }]),
    'section:tags': slice([{ id: 3, label: 'Fire', alert: '', led: '' }]),
    'section:access': slice(ACCESS),
    'section:apikeys': slice(APIKEYS),
    'section:downstreams': slice(DOWNSTREAMS),
    'section:dirwatch': slice([{ id: 1, order: 1, directory: '/var/spool/first', extension: 'mp3', mask: '#SYS #TG', type: 'default', systemId: null, siteId: null, talkgroupId: null, frequency: 1, delay: 2000, disabled: false }]),
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

function passwordPanel() {
  return panels(attach('rdio-admin-tools')).find((panel) => texts(findAll(panel, (child) => child.className?.split(/\s+/).includes('row-title')))[0] === 'Change password');
}

function texts(nodes) {
  return nodes.map((node) => node.textContent);
}

function spanClass(node) {
  return node.className?.split(/\s+/).find((name) => name === 'field-title' || name === 'body');
}

// namedField pairs a field with its naming element: inner label, or row caption
function namedField(root, name) {
  const fields = findAll(root, (node) => node.tag === 'rdio-form-field');

  for (const field of fields) {
    const naming = findAll(field, (child) => child.tag === 'label' || spanClass(child))
      .find((el) => el.textContent === name);

    if (naming) return { field, naming };
  }

  const caption = findAll(root, (child) => spanClass(child)).find((el) => el.textContent === name);

  for (let node = caption?.parent; node; node = node.parent) {
    const field = findAll(node, (child) => child.tag === 'rdio-form-field')[0];

    if (field) return { field, naming: caption };
  }

  return undefined;
}

// controlFor resolves the field to its input, or the pick-list primitive's select
function controlFor(root, name) {
  const found = namedField(root, name);

  if (!found) return undefined;

  return findAll(found.field, (child) => child.tag === 'select')[0]
    ?? findAll(found.field, (child) => ['input', 'textarea', 'rdio-select'].includes(child.tag))[0];
}

function ariaRequired(root, name) {
  return controlFor(root, name)?.getAttribute('aria-required') ?? null;
}

// nameCarriesMark reads the mark on the named element, marked or not
function nameCarriesMark(root, name) {
  return requiredMark(namedField(root, name)?.naming);
}

// ---------------------------------------------------------------------------
// A marked field's control answers required, the pick lists included.
// ---------------------------------------------------------------------------

const MARKED_CONTROLS = [
  ['systems row', () => addRow(attach('rdio-admin-systems')), ['Id', 'Label']],
  ['sites row', () => addRow(attach('rdio-admin-sites')), ['Id', 'Label']],
  ['talkgroups row', () => addRow(attach('rdio-admin-talkgroups')), ['Id', 'Label', 'Name', 'Tag']],
  ['units row', () => addRow(attach('rdio-admin-units')), ['Label']],
  ['groups row', () => addRow(attach('rdio-admin-groups')), ['Label']],
  ['tags row', () => addRow(attach('rdio-admin-tags')), ['Label']],
  ['access row', () => addRow(attach('rdio-admin-access')), ['Code', 'Ident']],
  ['API keys row', () => addRow(attach('rdio-admin-apikeys')), ['Ident', 'Key']],
  ['downstreams row', () => addRow(attach('rdio-admin-downstreams')), ['API key', 'URL']],
  ['dirwatch row', () => addRow(attach('rdio-admin-dirwatch')), ['Directory']],
  ['options panel', () => attach('rdio-admin-options'), ['Keypad Beep Style', 'Max Clients', 'Prune Days']],
  ['change password panel', passwordPanel, ['Current password', 'New password', 'Verify new password']],
];

for (const [name, surface, fields] of MARKED_CONTROLS) {
  test(`the marked fields of the ${name} announce themselves required`, () => {
    seed(getAdminContext().store);

    const root = surface();

    for (const field of fields) {
      assert.ok(nameCarriesMark(root, field), `${field} carries the mark on its name`);
      assert.equal(ariaRequired(root, field), 'true', `${field} announces its control as required`);
    }
  });
}

// ---------------------------------------------------------------------------
// A field the webapp accepts empty announces nothing.
// ---------------------------------------------------------------------------

const UNMARKED_CONTROLS = [
  ['systems row', () => addRow(attach('rdio-admin-systems')), ['Audio alert', 'Blacklists', 'Delay', 'Led Color', 'Type']],
  ['talkgroups row', () => addRow(attach('rdio-admin-talkgroups')), ['Audio alert', 'Delay', 'Frequency', 'Led Color', 'Type']],
  ['units row', () => addRow(attach('rdio-admin-units')), ['Id', 'Range from', 'Range to']],
  ['groups row', () => addRow(attach('rdio-admin-groups')), ['Audio alert', 'Led Color']],
  ['tags row', () => addRow(attach('rdio-admin-tags')), ['Audio alert', 'Led Color']],
  ['dirwatch row', () => addRow(attach('rdio-admin-dirwatch')), ['Extension', 'Mask', 'System', 'Talkgroup']],
  ['options panel', () => attach('rdio-admin-options'), ['Audio Conversion', 'Branding Label', 'Email Support']],
];

for (const [name, surface, fields] of UNMARKED_CONTROLS) {
  test(`the fields the ${name} accepts empty announce nothing`, () => {
    seed(getAdminContext().store);

    const root = surface();

    for (const field of fields) {
      assert.ok(!nameCarriesMark(root, field), `${field} carries no mark on its name`);
      assert.equal(ariaRequired(root, field), null, `${field} leaves its control unannounced`);
    }
  });
}

// the browser's native required attribute is its own validation, not the section's
