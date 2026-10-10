// The OpenAPI help box (T019, CA-017): every section card ends with the box,
// and the endpoints it names exist in docs/openapi-admin.json. The box is
// informational: the test reads what it renders, never what it requests.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/openapi-box.js';
import '../presentation/admin/rdio-admin-access.js';
import '../presentation/admin/rdio-admin-apikeys.js';
import '../presentation/admin/rdio-admin-dirwatch.js';
import '../presentation/admin/rdio-admin-downstreams.js';
import '../presentation/admin/rdio-admin-groups.js';
import '../presentation/admin/rdio-admin-logs.js';
import '../presentation/admin/rdio-admin-options.js';
import '../presentation/admin/rdio-admin-sites.js';
import '../presentation/admin/rdio-admin-systems.js';
import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-tags.js';
import '../presentation/admin/rdio-admin-tools.js';
import '../presentation/admin/rdio-admin-units.js';

import { findAll, texts } from './dom-shim.js';

const openapi = JSON.parse(readFileSync(new URL('../../../../../docs/openapi-admin.json', import.meta.url), 'utf8'));

const DOC_PATHS = Object.keys(openapi.paths)
  .filter((p) => !p.includes('{') || openapi.paths[p].get || openapi.paths[p].put)
  .filter((p) => openapi.paths[p].get || openapi.paths[p].put || openapi.paths[p].post);

function endpointKnown(endpoint) {
  const [method, path] = endpoint.split(' ');

  return DOC_PATHS.some((docPath) => {
    const pattern = new RegExp(`^${docPath.replace(/\{[^}]+\}/g, '[^/]+')}$`);

    return pattern.test(path) && (openapi.paths[docPath][method.toLowerCase()] !== undefined);
  });
}

function seed() {
  const { store } = getAdminContext();

  const state = { auth: { authenticated: true, docker: false, passwordNeedChange: false }, alerts: {} };

  for (const name of ['access', 'apikeys', 'dirwatch', 'downstreams', 'groups', 'options', 'systems', 'tags']) {
    state[`section:${name}`] = {
      baseline: name === 'options' ? {} : [],
      working: name === 'options' ? {} : [],
      dirty: false,
      invalid: false,
      conflict: false,
    };
  }

  const system = { id: 1, label: 'Metro', order: 1, blacklists: '', sites: [], talkgroups: [], units: [] };

  for (const level of ['sites', 'talkgroups', 'units']) {
    state[`subsection:${level}:1`] = { baseline: [], working: [], dirty: false, invalid: false, conflict: false };
  }

  state['section:systems'].working = [system];
  state['section:systems'].baseline = [system];

  store.setState(state);
}

function boxFor(section) {
  const box = document.createElement('rdio-admin-openapi-box');

  box.dataset.section = section;

  box.connectedCallback();

  return box;
}

function sectionBox(tag, section) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();

  const boxes = findAll(el, (node) => node.tag === 'rdio-admin-openapi-box');

  assert.equal(boxes.length, 1, `the ${section} section card does not end with the OpenAPI box`);

  boxes[0].connectedCallback();

  return boxes[0];
}

const SECTIONS = [
  ['rdio-admin-access', 'access'],
  ['rdio-admin-apikeys', 'apikeys'],
  ['rdio-admin-dirwatch', 'dirwatch'],
  ['rdio-admin-downstreams', 'downstreams'],
  ['rdio-admin-groups', 'groups'],
  ['rdio-admin-tags', 'tags'],
  ['rdio-admin-systems', 'systems'],
  ['rdio-admin-sites', 'sites'],
  ['rdio-admin-talkgroups', 'talkgroups'],
  ['rdio-admin-units', 'units'],
  ['rdio-admin-options', 'options'],
  ['rdio-admin-tools', 'tools'],
];

test('every section card ends with the OpenAPI box naming endpoints that exist in the OpenAPI document', () => {
  seed();

  const boxes = SECTIONS.map(([tag, section]) => sectionBox(tag, section));

  // The logs card is an innerHTML template: its box is the element tag the
  // template carries, and its content comes from the same shared map. The
  // template is assigned first in connectedCallback; the shim cannot build the
  // real rdio-select internals the rest of the callback wires, so the template
  // is read after the callback reaches that shim limit.
  const logs = new (customElements.get('rdio-admin-logs'))();

  try {
    logs.connectedCallback();
  } catch {
    // The shim stops at the first primitive it cannot build; the template is
    // already in place.
  }

  assert.ok(logs.innerHTML.includes('<rdio-admin-openapi-box data-section="logs">'), 'the logs card does not carry the OpenAPI box');

  boxes.push(boxFor('logs'));

  for (const box of boxes) {
    const endpoints = texts(findAll(box, (node) => node.tag === 'code'));

    assert.ok(endpoints.length >= 1, 'a section box names no endpoint');

    for (const endpoint of endpoints) {
      assert.ok(endpointKnown(endpoint), `a box names ${endpoint}, absent from docs/openapi-admin.json`);
    }

    const examples = findAll(box, (node) => node.tag === 'pre');

    assert.equal(examples.length, 1, 'a section box has no curl example');
    assert.ok(examples[0].textContent.startsWith('curl'), 'a section box example is not a curl call');
  }
});

test('the per-level boxes name the per-level endpoints', () => {
  seed();

  for (const [tag, level] of [['rdio-admin-sites', 'sites'], ['rdio-admin-talkgroups', 'talkgroups'], ['rdio-admin-units', 'units']]) {
    const endpoints = texts(findAll(sectionBox(tag, level), (node) => node.tag === 'code'));

    for (const endpoint of endpoints) {
      assert.ok(endpoint.includes(`/systems/{systemId}/${level}`), `the ${level} box does not name the per-level endpoint`);
    }
  }
});
