// the OpenAPI contract view: every path and method lands in exactly one group,
// the Config box splits by resource, and a new endpoint fails here until named

import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';

import { buildOpenApiGroups, contractEndpoints } from '../domain/openapi-view.js';

const contract = JSON.parse(readFileSync(new URL('../../../../../docs/openapi.json', import.meta.url), 'utf8'));

test('every endpoint of the contract lands in exactly one group', () => {
  const groups = buildOpenApiGroups(contract);

  const named = groups.flatMap((group) =>
    (group.subgroups ?? [{ endpoints: group.endpoints }]).flatMap((sub) => sub.endpoints.map((e) => `${e.method} ${e.path}`)),
  );

  const expected = contractEndpoints(contract);

  assert.equal(named.length, new Set(named).size, 'an endpoint is named by more than one group');
  assert.deepEqual([...named].sort(), [...expected].sort(), 'the groups do not name exactly the endpoints of the contract');
});

test('the groups carry the admin-facing names of the surface', () => {
  const groups = buildOpenApiGroups(contract);

  assert.deepEqual(groups.map((group) => group.title), [
    'Authentication',
    'Config',
    'Alerts',
    'Status',
    'Logs',
    'Calls',
    'Password',
    'Users',
    'Capture',
    'Applications',
  ]);
});

// the live status read is its own group: a server read, not a saved resource
test('the Status group names the one live status read', () => {
  const groups = buildOpenApiGroups(contract);

  const status = groups.find((group) => group.title === 'Status');

  assert.ok(status, 'the view names no Status group: the status read lands in no group');

  assert.deepEqual(status.endpoints.map((endpoint) => `${endpoint.method} ${endpoint.path}`), ['GET /api/admin/status']);
});

// the three call history endpoints form their own group
test('the Calls group names the three call history endpoints', () => {
  const groups = buildOpenApiGroups(contract);

  const calls = groups.find((group) => group.title === 'Calls');

  assert.ok(calls, 'the view names no Calls group: the call history endpoints land in no group');

  assert.deepEqual(calls.endpoints.map((endpoint) => `${endpoint.method} ${endpoint.path}`).sort(), [
    'GET /api/calls/{callId}',
    'GET /api/calls/{callId}/audio',
    'POST /api/calls/search',
  ]);
});

test('the Config group splits its endpoints by resource', () => {
  const groups = buildOpenApiGroups(contract);

  const config = groups.find((group) => group.title === 'Config');

  assert.deepEqual(config.subgroups.map((sub) => sub.label), ['config', 'sites', 'talkgroups', 'units']);
});

test('every endpoint carries its method, path, and the summary the contract states', () => {
  const groups = buildOpenApiGroups(contract);

  const endpoints = groups.flatMap((group) =>
    (group.subgroups ?? [{ endpoints: group.endpoints }]).flatMap((sub) => sub.endpoints),
  );

  const login = endpoints.find((endpoint) => endpoint.path === '/api/admin/login');

  assert.equal(login.method, 'POST');
  assert.equal(login.summary, 'Log in and obtain a JWT');
});

const allEndpoints = (groups) =>
  groups.flatMap((group) =>
    (group.subgroups ?? [{ endpoints: group.endpoints }]).flatMap((sub) => sub.endpoints),
  );

const endpointOf = (groups, path, method) =>
  allEndpoints(groups).find((e) => e.path === path && e.method === method);

test('every endpoint states the description the contract gives it', () => {
  const groups = buildOpenApiGroups(contract);

  for (const endpoint of allEndpoints(groups)) {
    assert.notEqual(endpoint.description, undefined, `${endpoint.method} ${endpoint.path} states no description`);
    assert.notEqual(endpoint.description, '', `${endpoint.method} ${endpoint.path} states an empty description`);
  }

  assert.equal(
    endpointOf(groups, '/api/admin/login', 'POST').description,
    'Exchange the administrator password for a JWT. Rate limited to three attempts per address, cleared on success.',
  );
});

test('an operation whose contract entry states no description falls back to its summary', () => {
  const groups = buildOpenApiGroups({
    paths: {
      '/api/admin/user-add': { post: { summary: 'Add an access user', responses: { '200': { description: 'User saved' } } } },
    },
    components: { schemas: {} },
  });

  assert.equal(endpointOf(groups, '/api/admin/user-add', 'POST').description, 'Add an access user');
});

test('the path parameter the contract declares for a path reaches the endpoints under it', () => {
  const endpoint = endpointOf(buildOpenApiGroups(contract), '/api/admin/config/{section}', 'GET');

  // allowed values travel with the parameter, shown on the expanded endpoint
  assert.deepEqual(endpoint.params, [{
    name: 'section',
    in: 'path',
    required: true,
    type: 'string',
    enum: ['access', 'apikeys', 'dirwatch', 'downstreams', 'groups', 'options', 'systems', 'tags'],
    description: 'The configuration section to read or save.',
  }]);
});

test('an endpoint whose path and operation declare no parameter states none', () => {
  assert.deepEqual(endpointOf(buildOpenApiGroups(contract), '/api/admin/login', 'POST').params, []);
});

test('the request body names its content type and the fields of the schema it states', () => {
  const endpoint = endpointOf(buildOpenApiGroups(contract), '/api/admin/login', 'POST');

  assert.deepEqual(endpoint.body, [{
    contentType: 'application/json',
    schema: 'LoginRequest',
    fields: [{ name: 'password', type: 'string', required: true, description: '' }],
  }]);
});

test('the capture endpoint lists the form fields the contract describes, and marks the required ones', () => {
  const body = endpointOf(buildOpenApiGroups(contract), '/api/call-upload', 'POST').body;

  assert.equal(body.length, 1);
  assert.equal(body[0].contentType, 'multipart/form-data');

  const fields = new Map(body[0].fields.map((f) => [f.name, f]));

  assert.equal(fields.get('key').required, true);
  assert.equal(fields.get('audio').required, true);
  assert.equal(fields.get('audio').type, 'string (binary)');
  assert.equal(fields.get('key').description, 'The apikey credential the upload is checked against.');
  assert.ok(fields.has('audioLength'), 'the form field the contract documents must be listed');

  for (const field of body[0].fields) {
    assert.notEqual(field.type, undefined, `the form field ${field.name} states no type`);
  }
});

test('every response code the contract states is listed with its description', () => {
  const endpoint = endpointOf(buildOpenApiGroups(contract), '/api/admin/login', 'POST');

  assert.deepEqual(endpoint.responses.map((r) => r.code), ['200', '400', '401', '405']);

  for (const response of endpoint.responses) {
    assert.notEqual(response.description, undefined, `response ${response.code} states no description`);
    assert.notEqual(response.description, '', `response ${response.code} states an empty description`);
  }
});

test('an endpoint whose operation states no request body states no body', () => {
  const endpoint = endpointOf(buildOpenApiGroups(contract), '/api/admin/alerts', 'GET');

  assert.deepEqual(endpoint.body, []);
});

test('every schema a request body references in the contract is named by the view', () => {
  const groups = buildOpenApiGroups(contract);

  const referenced = new Set();

  for (const operations of Object.values(contract.paths)) {
    for (const operation of Object.values(operations)) {
      for (const media of Object.values(operation?.requestBody?.content ?? {})) {
        for (const ref of JSON.stringify(media.schema).matchAll(/#\/components\/schemas\/(\w+)/g)) {
          referenced.add(ref[1]);
        }
      }
    }
  }

  const named = new Set(allEndpoints(groups).flatMap((e) => e.body.flatMap((b) => b.schema.split(' / ').map((name) => name.replace(/\[\]$/, '')))));

  for (const schema of referenced) {
    assert.ok(named.has(schema), `the view never names ${schema}, which a request body references`);
  }
});

test('a reference to a schema the contract does not define is named as unresolved, never dropped', () => {
  const groups = buildOpenApiGroups({
    paths: {
      '/api/admin/user-add': {
        post: {
          summary: 'Add a user',
          requestBody: { content: { 'application/json': { schema: { $ref: '#/components/schemas/NotInContract' } } } },
          responses: { '200': { description: 'Done' } },
        },
      },
    },
    components: { schemas: {} },
  });

  const endpoint = endpointOf(groups, '/api/admin/user-add', 'POST');

  assert.deepEqual(endpoint.body, [{
    contentType: 'application/json',
    schema: 'NotInContract',
    fields: [],
    unresolved: true,
  }]);
});
