// T034: the OpenAPI document for the admin REST surface must parse with
// Node's built-in JSON support and cover every endpoint declared in
// contract-server-protocol.md §3, including the 8 per-section pairs.

import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

import { fileURLToPath } from 'node:url';

const DOC_PATH = fileURLToPath(new URL('../../../../docs/specs/FEAT-002-vanilla-client-ng/openapi-admin.json', import.meta.url));

const doc = JSON.parse(readFileSync(DOC_PATH, 'utf8'));

const SECTIONS = ['access', 'apikeys', 'dirwatch', 'downstreams', 'groups', 'options', 'systems', 'tags'];

test('openapi-admin.json declares OpenAPI 3.1 with a title', () => {
  assert.equal(doc.openapi, '3.1.0');
  assert.ok(doc.info?.title);
});

test('every legacy admin endpoint from contract §3 is documented', () => {
  const expected = {
    '/api/admin/login': ['post'],
    '/api/admin/logout': ['post'],
    '/api/admin/config': ['get', 'put'],
    '/api/admin/alerts': ['get'],
    '/api/admin/logs': ['post'],
    '/api/admin/password': ['post'],
  };

  for (const [path, methods] of Object.entries(expected)) {
    for (const method of methods) {
      assert.ok(doc.paths?.[path]?.[method], `missing ${method.toUpperCase()} ${path}`);
    }
  }
});

test('all 8 per-section GET/PUT pairs are documented', () => {
  for (const section of SECTIONS) {
    const item = doc.paths?.['/api/admin/config/{section}'];

    assert.ok(item?.get, `missing GET /api/admin/config/${section}`);
    assert.ok(item?.put, `missing PUT /api/admin/config/${section}`);

    const enumValues = item.parameters.find((p) => p.name === 'section')?.schema?.enum;

    assert.deepEqual(enumValues, SECTIONS, 'section path parameter enum mismatch');
  }
});

test('raw-JWT bearer-less security scheme is declared and applied', () => {
  const scheme = doc.components?.securitySchemes?.adminJwt;

  assert.equal(scheme?.type, 'apiKey');
  assert.equal(scheme?.in, 'header');
  assert.equal(scheme?.name, 'Authorization');

  for (const [path, item] of Object.entries(doc.paths ?? {})) {
    if (path === '/api/admin/login') {
      continue; // login is the only endpoint that works without a token
    }

    for (const [method, operation] of Object.entries(item)) {
      if (!['get', 'put', 'post', 'patch', 'delete'].includes(method)) continue;

      assert.deepEqual(operation.security, [{ adminJwt: [] }], `${method.toUpperCase()} ${path} missing adminJwt security`);
    }
  }
});

test('section responses reference the config entity schemas', () => {
  const sectionItem = doc.paths['/api/admin/config/{section}'];

  const collectRefs = (node, acc = new Set()) => {
    if (!node || typeof node !== 'object') return acc;
    if (typeof node.$ref === 'string') acc.add(node.$ref.split('/').pop());
    for (const value of Object.values(node)) collectRefs(value, acc);
    return acc;
  };

  const refs = new Set();
  for (const method of ['get', 'put']) {
    for (const ref of collectRefs(sectionItem[method].responses['200'].content['application/json'].schema)) refs.add(ref);
  }

  for (const entity of ['Access', 'Apikey', 'Dirwatch', 'Downstream', 'Group', 'Tag', 'System', 'Options']) {
    assert.ok(refs.has(entity), `section response does not reference schema ${entity}`);
    assert.ok(doc.components?.schemas?.[entity], `missing schema ${entity}`);
  }
});
