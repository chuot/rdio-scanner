// The OpenAPI section's view of the contract (FEAT-024, CA-003): every path and
// method of docs/openapi.json lands in exactly one group, the Config box splits
// by resource, and a contract edit that adds an endpoint fails this test until
// the view names it — the section can only ever show what the server answers.

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
    'Logs',
    'Password',
    'Users',
    'Capture',
    'Applications',
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
