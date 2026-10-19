// Every control a list section's rows let the operator edit must announce a name, and the name
// must carry the words the row shows beside the control. A pick list that names its control on the
// host element and never moves the name onto the control announces nothing, and a field whose label
// is attached to no control leaves a screen reader reading a control it cannot identify. The
// readings below walk every form field each section renders and name the control the accessibility
// tree would speak for.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-access.js';
import '../presentation/admin/rdio-admin-apikeys.js';
import '../presentation/admin/rdio-admin-dirwatch.js';
import '../presentation/admin/rdio-admin-downstreams.js';
import '../presentation/admin/rdio-admin-groups.js';
import '../presentation/admin/rdio-admin-tags.js';

import { connectFields, findAll } from './dom-shim.js';

function slice(baseline) {
  return { baseline, working: structuredClone(baseline), dirty: false, invalid: false, conflict: false };
}

const SYSTEMS = [
  { id: 1, order: 1, label: 'Metro', systemRef: 1, sites: [], talkgroups: [{ id: 11, talkgroupRef: 11, label: 'Fire dispatch' }], units: [] },
];

const SECTIONS = [
  {
    name: 'groups', tag: 'rdio-admin-groups', key: 'section:groups',
    seedRows: [{ id: 1, order: 1, label: 'Dispatch', alert: 'storm', led: 'blue' }, { id: 2, order: 2, label: 'Console' }],
  },
  {
    name: 'tags', tag: 'rdio-admin-tags', key: 'section:tags',
    seedRows: [{ id: 4, order: 1, label: 'Fire', alert: 'storm', led: 'red' }, { id: 5, order: 2, label: 'Car Talk' }],
  },
  {
    name: 'access', tag: 'rdio-admin-access', key: 'section:access',
    seedRows: [{ id: 7, order: 1, code: 'front-gate', ident: 'Front desk', systems: '*' }, { id: 8, order: 2, code: 'shop-line', ident: 'Shop door', systems: '*' }],
  },
  {
    name: 'api key', tag: 'rdio-admin-apikeys', key: 'section:apikeys',
    seedRows: [{ id: 10, order: 1, disabled: false, key: 'key-one', ident: 'Recorder', systems: '*' }, { id: 11, order: 2, disabled: true, key: 'key-two', ident: 'Booth line', systems: '*' }],
  },
  {
    name: 'downstream', tag: 'rdio-admin-downstreams', key: 'section:downstreams',
    seedRows: [{ id: 13, order: 1, disabled: false, url: 'https://one.example/api', apikey: 'key-one', systems: '*' }, { id: 14, order: 2, disabled: false, url: 'https://two.example/api', apikey: 'key-two', systems: '*' }],
  },
  {
    name: 'dirwatch', tag: 'rdio-admin-dirwatch', key: 'section:dirwatch',
    seedRows: [
      { id: 16, order: 1, delay: 2000, deleteAfter: true, directory: '/var/record/metro', disabled: false, extension: 'mp3', frequency: 1, mask: '#SYS_#TG', type: 'default' },
      { id: 17, order: 2, delay: 2000, deleteAfter: true, directory: '/var/record/county', disabled: false, extension: 'mp3', frequency: 1, mask: '#SYS_#TG', type: 'default' },
    ],
  },
];

function seed(section) {
  const store = getAdminContext().store;

  const state = {
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    alerts: { storm: [{ begin: 0, end: 0.04, frequency: 440, type: 'square' }] },
    'section:systems': slice(SYSTEMS),
  };

  for (const other of SECTIONS) {
    state[other.key] = other.key === section.key ? slice(section.seedRows) : slice([]);
  }

  store.setState(state);
}

function attach(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

// The words the row shows as this field's name: the label inside the form field where the section
// uses one, the caption span named beside the control where the row is laid out by hand.
function fieldTitle(field) {
  const label = findAll(field, (node) => node.tag === 'label')[0];

  if (label) return label.textContent.trim();

  for (let node = field.parent; node; node = node.parent) {
    const title = findAll(node, (child) => child.className?.split(/\s+/).includes('field-title'))[0];

    if (title) return title.textContent.trim();
  }

  return '';
}

// The name a screen reader is handed for the control itself: the name written on the control, or
// the text of the label attached to it. A name sitting on any other element — the pick list's host
// around the control, say — announces nothing and is not read.
function announcedName(field, control) {
  const written = control.getAttribute('aria-label') ?? control.getAttribute('aria-labelledby');

  if (written) return written.trim();

  for (const label of findAll(field, (node) => node.tag === 'label')) {
    const attachedTo = label.htmlFor ?? label.getAttribute('for');

    if (control.id && attachedTo === control.id) return label.textContent.trim();
  }

  return '';
}

for (const section of SECTIONS) {
  test(`the ${section.name} rows name every control they let the operator edit`, () => {
    seed(section);

    const el = attach(section.tag);

    const fields = findAll(el, (node) => node.tag === 'rdio-form-field');

    assert.ok(fields.length > 0, `the ${section.name} section rendered no form fields to read`);

    for (const field of fields) {
      const control = findAll(field, (node) => ['input', 'select', 'textarea'].includes(node.tag))[0];
      const title = fieldTitle(field);

      assert.ok(control, `the ${section.name} field ${JSON.stringify(title)} carries no control the operator can edit`);

      const announced = announcedName(field, control);

      assert.ok(announced !== '', `the ${section.name} ${control.tag} for ${JSON.stringify(title)} announces no name a screen reader can read`);

      if (title !== '') {
        assert.ok(
          announced.toLowerCase().includes(title.toLowerCase()),
          `the ${section.name} ${control.tag} announces ${JSON.stringify(announced)}, which does not carry the name the row shows (${JSON.stringify(title)})`,
        );
      }
    }
  });
}
