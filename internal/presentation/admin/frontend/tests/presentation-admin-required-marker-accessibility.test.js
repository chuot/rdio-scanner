// The mark a screen reader hears. The asterisk the operator reads is drawn by the stylesheet, so the
// assistive technology reading the page needs the same fact on the control itself: `aria-required`
// true on a field the webapp insists on, and nothing at all on a field it accepts empty. The mark
// names the declaration, so the attribute follows the declaration and not the row's state.
//
// The control is what carries it: the box the operator types in, and for a pick list the select the
// primitive builds inside it — the element the label is attached to and the screen reader announces.

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

// The field a name belongs to. A field built with a label inside it answers for its own name; a field
// a hand-built row names with a caption beside it is the field that sits in the same row as that
// caption, which is the shape the options panel and the hand-built rows use.
function fieldForName(root, name) {
  const fields = findAll(root, (node) => node.tag === 'rdio-form-field');

  const named = fields.find((field) => findAll(field, (child) => child.tag === 'label' || spanClass(child))
    .some((el) => el.textContent === name));

  if (named) return named;

  const caption = findAll(root, (child) => spanClass(child)).find((el) => el.textContent === name);

  for (let node = caption?.parent; node; node = node.parent) {
    const field = findAll(node, (child) => child.tag === 'rdio-form-field')[0];

    if (field) return field;
  }

  return undefined;
}

// The control the field resolves to: the box the operator types in, or the select the pick-list
// primitive builds inside the field, which is the element a label is attached to and a screen reader
// announces.
function controlFor(root, name) {
  const field = fieldForName(root, name);

  if (!field) return undefined;

  return findAll(field, (child) => child.tag === 'select')[0]
    ?? findAll(field, (child) => ['input', 'textarea', 'rdio-select'].includes(child.tag))[0];
}

function ariaRequired(root, name) {
  return controlFor(root, name)?.getAttribute('aria-required') ?? null;
}

// The name the field carries the mark on, marked or not: the reading pairs the mark on the name with
// the attribute on the control, so a field whose name carries no mark cannot answer with a required
// control, and a marked name cannot leave its control unannounced.
function nameCarriesMark(root, name) {
  const field = fieldForName(root, name);

  if (!field) return false;

  return findAll(field, (child) => child.tag === 'label' || spanClass(child))
    .some((el) => el.textContent === name && requiredMark(el));
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

// The native attribute is the browser's own validation and is not this declaration: the mark is the
// section's, and the primitive's attribute is the reading of it. The fields the sections declare are
// read above; the fields they do not are read below, and the browser's own attribute is left to the
// forms that already carried it.
