// The OpenAPI submenu: the
// section renders the endpoint groups derived from docs/openapi.json through the
// shared box. Each endpoint is a detail row: its summary line names the method,
// the path, and the summary, and it opens on the description, the parameters, the
// request body fields, and the response codes the contract states. The rows are
// closed by default, so the page reads as a table of contents. The auth sentence
// is stated once in the card intro. A contract that cannot be fetched renders one
// honest caption. No other section card carries an OpenAPI box. The boxes are
// informational: the test reads what they render, never what they request.

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

// The endpoint rows are the detail elements the box renders, one per endpoint.
// A box, a list of boxes, or the card itself all read as a place to look in.
const places = (root) => (Array.isArray(root) ? root : [root]);

const rows = (root) => places(root).flatMap((node) => findAll(node, (n) => n.tag === 'details'));

const boxTitle = (box) => texts(findAll(box, (node) => node.className === 'box-title').flatMap((p) => findAll(p, (node) => node.tag === 'span')))[0];

// The box puts a zero-width break opportunity after every path separator so a long
// path wraps on a narrow viewport. The path the administrator reads is the path of
// the contract, separators included.
const paths = (root) => texts(findAll(root, (node) => node.className === 'endpoint-path')).map((text) => text.replace(/\u200B/g, ''));

// allText reads everything a node states, containers included: the detail checks
// ask what the administrator reads when the row opens, not which element carries it.
const allText = (root) => texts(findAll(root, () => true)).join('\n');

const rowFor = (root, path, method) =>
  rows(root).find(
    (row) =>
      paths(row).includes(path) &&
      texts(findAll(row, (node) => node.className.startsWith('method '))).includes(method),
  );

function renderBox(group) {
  const box = new (customElements.get('rdio-admin-openapi-box'))();

  box.group = group;

  box.connectedCallback();

  return box;
}

test('the OpenAPI card renders one box per group, each endpoint naming method, path, and summary', async () => {
  const { boxes } = await openApiCard();

  assert.equal(boxes.length, 9, 'the OpenAPI card does not carry the nine contract groups');

  const titles = boxes.map(boxTitle);

  assert.deepEqual(titles, ['Authentication', 'Config', 'Alerts', 'Logs', 'Calls', 'Password', 'Users', 'Capture', 'Applications']);

  const login = rowFor(boxes[0], '/api/admin/login', 'POST');

  assert.ok(login, 'the Authentication box does not render the login endpoint as a row');
  assert.deepEqual(texts(findAll(login, (node) => node.className.startsWith('method '))), ['POST']);
  assert.deepEqual(paths(login), ['/api/admin/login']);
  assert.deepEqual(texts(findAll(login, (node) => node.className === 'summary')), ['Log in and obtain a JWT']);
});

// The call history over HTTP. The card shows what the server answers, so the three call
// endpoints appear in their own box, read between the log view and the password change.
test('the Calls box renders the three call history endpoints', async () => {
  const { boxes } = await openApiCard();

  const calls = boxes.find((box) => boxTitle(box) === 'Calls');

  assert.ok(calls, 'the OpenAPI card carries no Calls box');

  assert.deepEqual(paths(calls).sort(), ['/api/calls/search', '/api/calls/{callId}', '/api/calls/{callId}/audio']);

  const search = rowFor(calls, '/api/calls/search', 'POST');

  assert.ok(search, 'the Calls box does not render the search endpoint as a row');
  assert.deepEqual(texts(findAll(search, (node) => node.className === 'summary')), ['Search the recorded calls']);
});

test('the OpenAPI card names every endpoint of the contract and nothing else', async () => {
  const { boxes } = await openApiCard();

  const named = boxes.flatMap((box) => paths(box));

  const contractPaths = Object.keys(contract.paths);

  for (const path of named) {
    assert.ok(contractPaths.includes(path), `the card names ${path}, absent from docs/openapi.json`);
  }

  for (const path of contractPaths) {
    assert.ok(named.includes(path), `the card does not name ${path}, an endpoint of the contract`);
  }
});

test('every endpoint of the contract is a row, and every row is closed by default', async () => {
  const { boxes } = await openApiCard();

  const endpoints = Object.entries(contract.paths).flatMap(([path, operations]) =>
    Object.keys(operations).filter((method) => ['get', 'post', 'put', 'delete', 'patch'].includes(method)).map((method) => [path, method.toUpperCase()]),
  );

  const all = boxes.flatMap((box) => rows(box));

  assert.equal(all.length, endpoints.length, 'the boxes do not render one row per endpoint of the contract');

  for (const [path, method] of endpoints) {
    assert.ok(rowFor(boxes, path, method), `the card does not render a row for ${method} ${path}`);
  }

  for (const row of all) {
    assert.equal(row.getAttribute('open'), null, `the row for ${paths(row)[0]} opens by default`);
  }
});

test('a row opens on the description, the request body, and the responses the contract states', async () => {
  const { boxes } = await openApiCard();

  const login = rowFor(boxes, '/api/admin/login', 'POST');

  const detail = findAll(login, (node) => node.className === 'endpoint-detail')[0];

  assert.ok(detail, 'the login row carries no detail to open');

  const body = allText(detail);

  assert.ok(
    body.includes('Exchange the administrator password for a JWT. Rate limited to three attempts per address, cleared on success.'),
    'the login detail does not state the description the contract gives the operation',
  );

  assert.ok(body.includes('application/json'), 'the login detail does not name the request body content type');
  assert.ok(body.includes('LoginRequest'), 'the login detail does not name the request body schema');
  assert.ok(body.includes('password'), 'the login detail does not name the request body field');

  for (const code of ['200', '400', '401', '405']) {
    assert.ok(body.includes(code), `the login detail does not list response ${code}`);
  }

  assert.ok(body.includes('Wrong password or rate limit exceeded (3 attempts per IP, counter cleared on success)'), 'the login detail does not state the response description');
});

test('a row names the parameters the contract declares for its path', async () => {
  const { boxes } = await openApiCard();

  const read = rowFor(boxes, '/api/admin/config/{section}', 'GET');

  const detail = findAll(read, (node) => node.className === 'endpoint-detail')[0];

  const body = allText(detail);

  assert.ok(body.includes('section'), 'the detail does not name the path parameter the contract declares');
  assert.ok(body.includes('The configuration section to read or save.'), 'the detail does not state the path parameter description');
  assert.ok(body.includes('access'), 'the detail does not name the values the path parameter accepts');
});

test('an endpoint with no request body states no request body', async () => {
  const { boxes } = await openApiCard();

  const alerts = rowFor(boxes, '/api/admin/alerts', 'GET');

  const detail = findAll(alerts, (node) => node.className === 'endpoint-detail')[0];

  assert.ok(detail, 'the alerts row carries no detail to open');
  assert.equal(findAll(detail, (node) => node.className === 'request-body').length, 0, 'the alerts detail states a request body the contract does not declare');
});

test('the capture row lists the form fields the contract describes, marking the required ones', async () => {
  const { boxes } = await openApiCard();

  const capture = rowFor(boxes, '/api/call-upload', 'POST');

  const fields = findAll(capture, (node) => node.className === 'field-name');

  const names = new Map(fields.map((field) => [field.textContent, field]));

  for (const name of ['key', 'audio', 'audioFilename', 'audioLength', 'channelType']) {
    assert.ok(names.has(name), `the capture detail does not name the form field ${name}`);
  }

  assert.equal(names.get('key').getAttribute('data-required'), 'true', 'the required form field key is not marked');
  assert.equal(names.get('audio').getAttribute('data-required'), 'true', 'the required form field audio is not marked');
  assert.equal(names.get('audioFilename').getAttribute('data-required'), null, 'an optional form field is marked required');
});

test('a request body whose schema the contract does not define is named as unresolved', () => {
  const box = renderBox({
    title: 'Users',
    icon: 'person',
    endpoints: [{
      method: 'POST',
      path: '/api/admin/user-add',
      summary: 'Add an access user',
      description: 'Add an access user to the access list.',
      params: [],
      body: [{ contentType: 'application/json', schema: 'NotInContract', fields: [], unresolved: true }],
      responses: [{ code: '200', description: 'User saved' }],
    }],
  });

  const detail = allText(findAll(box, (node) => node.className === 'endpoint-detail')[0]);

  assert.ok(detail.includes('NotInContract'), 'the detail drops the schema the contract does not define');
  assert.ok(detail.includes('the contract does not define it'), 'the detail does not say the schema is not defined by the contract');
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

  const config = boxes.find((box) => boxTitle(box) === 'Config');

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
