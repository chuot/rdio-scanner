// The API entrypoint submenu (FEAT-018, CA-005): one API section card lists every
// OpenAPI entry point in seven groups, each group with one curl example, and every
// endpoint it names exists in docs/openapi-admin.json. No other section card
// carries an OpenAPI box. The boxes are informational: the test reads what they
// render, never what they request.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/openapi-box.js';
import '../presentation/admin/rdio-admin-api.js';
import '../presentation/admin/rdio-admin-access.js';
import '../presentation/admin/rdio-admin-apikeys.js';
import '../presentation/admin/rdio-admin-dirwatch.js';
import '../presentation/admin/rdio-admin-downstreams.js';
import '../presentation/admin/rdio-admin-groups.js';
import '../presentation/admin/rdio-admin-ingestion.js';
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

const DOC_PATHS = Object.keys(openapi.paths);

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

const GROUPS = ['authentication', 'config', 'alerts', 'logs', 'password', 'users', 'ingestion'];

function apiBoxes() {
  seed();

  const el = new (customElements.get('rdio-admin-api'))();

  el.connectedCallback();

  const boxes = findAll(el, (node) => node.tag === 'rdio-admin-openapi-box');

  assert.equal(boxes.length, GROUPS.length, 'the API card does not carry the seven entrypoint groups');

  for (const box of boxes) box.connectedCallback();

  return boxes;
}

function endpointsOf(box) {
  return texts(findAll(box, (node) => node.tag === 'code'));
}

test('the API card lists every entry point in seven groups, all known to the OpenAPI document', () => {
  const boxes = apiBoxes();

  for (const box of boxes) {
    const endpoints = endpointsOf(box);

    assert.ok(endpoints.length >= 1, 'a group names no endpoint');

    for (const endpoint of endpoints) {
      assert.ok(endpointKnown(endpoint), `a group names ${endpoint}, absent from docs/openapi-admin.json`);
    }

    const examples = findAll(box, (node) => node.tag === 'pre');

    assert.equal(examples.length, 1, 'a group has no curl example');
    assert.ok(examples[0].textContent.startsWith('curl'), 'a group example is not a curl call');
  }
});

test('the Authentication group names login and logout, the Ingestion group names both capture endpoints', () => {
  const byGroup = Object.fromEntries(apiBoxes().map((box) => [box.dataset.group, endpointsOf(box)]));

  assert.deepEqual(Object.keys(byGroup).sort(), [...GROUPS].sort(), 'the API card does not carry the named groups');

  assert.ok(byGroup.authentication.includes('POST /api/admin/login'), 'the Authentication group does not name login');
  assert.ok(byGroup.authentication.includes('POST /api/admin/logout'), 'the Authentication group does not name logout');

  for (const level of ['sites', 'talkgroups', 'units']) {
    assert.ok(byGroup.config.includes(`GET /api/admin/config/systems/{systemId}/${level}`), `the Config group does not name GET the ${level} level`);
    assert.ok(byGroup.config.includes(`PUT /api/admin/config/systems/{systemId}/${level}`), `the Config group does not name PUT the ${level} level`);
  }

  assert.ok(byGroup.ingestion.includes('POST /api/call-upload'), 'the Ingestion group does not name the capture endpoint');
  assert.ok(byGroup.ingestion.includes('POST /api/trunk-recorder-call-upload'), 'the Ingestion group does not name the trunk-recorder endpoint');
});

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
  ['rdio-admin-ingestion', 'ingestion'],
];

test('no other section card carries an OpenAPI box', () => {
  seed();

  for (const [tag, section] of SECTIONS) {
    const el = new (customElements.get(tag))();

    try {
      el.connectedCallback();
    } catch {
      // The shim stops at the first primitive it cannot build; the card is already in place.
    }

    assert.equal(findAll(el, (node) => node.tag === 'rdio-admin-openapi-box').length, 0, `the ${section} section card still carries an OpenAPI box`);
    assert.ok(!el.innerHTML.includes('rdio-admin-openapi-box'), `the ${section} card template still carries an OpenAPI box`);
  }
});

test('the logs card template carries no OpenAPI box', () => {
  seed();

  const logs = new (customElements.get('rdio-admin-logs'))();

  try {
    logs.connectedCallback();
  } catch {
    // The shim stops at the first primitive it cannot build; the template is already in place.
  }

  assert.ok(!logs.innerHTML.includes('rdio-admin-openapi-box'), 'the logs card still carries the OpenAPI box');
});
