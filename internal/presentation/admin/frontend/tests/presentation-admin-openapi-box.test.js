// The OpenAPI submenu (FEAT-018 CA-005, FEAT-024 CA-003): the section renders the
// endpoint groups derived from docs/openapi.json — method, path, and summary per
// endpoint — through the shared box. The auth sentence is stated once in the card
// intro. A contract that cannot be fetched renders one honest caption. No other
// section card carries an OpenAPI box. The boxes are informational: the test reads
// what they render, never what they request.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/openapi-box.js';
import '../presentation/admin/rdio-admin-openapi.js';
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

const contract = JSON.parse(readFileSync(new URL('../../../../../docs/openapi.json', import.meta.url), 'utf8'));

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

async function openApiCard(fetchImpl) {
  seed();

  globalThis.fetch = fetchImpl ?? (async () => ({ ok: true, json: async () => contract }));

  const el = new (customElements.get('rdio-admin-openapi'))();

  await el.connectedCallback();

  delete globalThis.fetch;

  const boxes = findAll(el, (node) => node.tag === 'rdio-admin-openapi-box');

  for (const box of boxes) box.connectedCallback();

  return { el, boxes };
}

test('the OpenAPI card renders one box per group, each endpoint with method, path, and summary', async () => {
  const { boxes } = await openApiCard();

  assert.equal(boxes.length, 8, 'the OpenAPI card does not carry the eight contract groups');

  const titles = boxes.map((box) => texts(findAll(box, (node) => node.className === 'box-title').flatMap((p) => findAll(p, (node) => node.tag === 'span')))[0]);

  assert.deepEqual(titles, ['Authentication', 'Config', 'Alerts', 'Logs', 'Password', 'Users', 'Capture', 'Applications']);

  const login = findAll(boxes[0], (node) => node.tag === 'li')[0];

  assert.deepEqual(texts(findAll(login, (node) => node.className.startsWith('method '))), ['POST']);
  assert.deepEqual(texts(findAll(login, (node) => node.tag === 'code')), ['/api/admin/login']);
  assert.deepEqual(texts(findAll(login, (node) => node.className === 'summary')), ['Log in and obtain a JWT']);
});

test('the OpenAPI card names every endpoint of the contract and nothing else', async () => {
  const { boxes } = await openApiCard();

  const paths = boxes.flatMap((box) => texts(findAll(box, (node) => node.tag === 'code')));

  const contractPaths = Object.keys(contract.paths);

  for (const path of paths) {
    assert.ok(contractPaths.includes(path), `the card names ${path}, absent from docs/openapi.json`);
  }

  for (const path of contractPaths) {
    assert.ok(paths.includes(path), `the card does not name ${path}, an endpoint of the contract`);
  }
});

test('the auth sentence is stated once on the page and no box repeats it', async () => {
  const { el, boxes } = await openApiCard();

  const sentence = 'Admin endpoints take the raw JWT in the Authorization header, no Bearer prefix; the capture endpoints authenticate with the key form field.';

  const captions = texts(findAll(el, (node) => node.className === 'caption'));

  assert.equal(captions.length, 1, 'the OpenAPI card intro is not a single caption');
  assert.ok(captions[0].includes(sentence), 'the OpenAPI card intro does not state the auth sentence');

  for (const box of boxes) {
    assert.equal(findAll(box, (node) => node.className === 'caption').length, 0, 'an entrypoint box carries a caption');
  }
});

test('the Config box groups its endpoints by resource', async () => {
  const { boxes } = await openApiCard();

  const config = boxes.find((box) => texts(findAll(box, (node) => node.className === 'box-title').flatMap((p) => findAll(p, (node) => node.tag === 'span'))).includes('Config'));

  const labels = texts(findAll(config, (node) => node.className === 'group-label'));

  assert.deepEqual(labels, ['config', 'sites', 'talkgroups', 'units'], 'the Config box does not group by resource');
});

test('a contract that cannot be fetched renders one honest caption, not a broken card', async () => {
  const { el, boxes } = await openApiCard(async () => {
    throw new TypeError('Failed to fetch');
  });

  assert.equal(boxes.length, 0, 'the card renders boxes without a contract');

  const captions = texts(findAll(el, (node) => node.className === 'caption'));

  assert.equal(captions.length, 2, 'the card does not render the intro and the unavailable caption');
  assert.ok(captions[1].includes('not available'), 'the failure caption does not name the problem');
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
