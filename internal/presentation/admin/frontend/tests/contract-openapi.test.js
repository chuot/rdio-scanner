// The OpenAPI document for the admin REST surface is the contract the administrator front end is
// written against. It must parse with Node's built-in JSON support and cover every endpoint the
// server answers, including the eight per-section pairs.

import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

import { fileURLToPath } from 'node:url';

const DOC_PATH = fileURLToPath(new URL('../../../../../docs/openapi-admin.json', import.meta.url));

const doc = JSON.parse(readFileSync(DOC_PATH, 'utf8'));

const SECTIONS = ['access', 'apikeys', 'dirwatch', 'downstreams', 'groups', 'options', 'systems', 'tags'];

test('the admin OpenAPI document declares OpenAPI 3.1 with a title', () => {
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

// The systems tree saves each level of one system on its own, so the document must
// carry the three per-level pairs with the system path parameter and the entity schema
// the level is made of.
const SUB_LEVELS = { sites: 'Site', talkgroups: 'Talkgroup', units: 'Unit' };

test('the six per-level system GET/PUT pairs are documented', () => {
  for (const [level, schema] of Object.entries(SUB_LEVELS)) {
    const path = '/api/admin/config/systems/{systemId}/' + level;
    const item = doc.paths?.[path];

    assert.ok(item?.get, `missing GET ${path}`);
    assert.ok(item?.put, `missing PUT ${path}`);

    const param = item.parameters.find((p) => p.name === 'systemId');

    assert.ok(param?.required, `${path} missing the required systemId path parameter`);

    for (const method of ['get', 'put']) {
      const response = item[method].responses['200'].content['application/json'].schema;

      assert.equal(response.items?.$ref, `#/components/schemas/${schema}`,
        `${method.toUpperCase()} ${path} 200 does not reference ${schema}`);

      assert.ok(doc.components?.schemas?.[schema], `missing schema ${schema}`);
    }

    const body = item.put.requestBody.content['application/json'].schema;

    assert.equal(body.items?.$ref, `#/components/schemas/${schema}`,
      `PUT ${path} body does not reference ${schema}`);

    for (const status of ['400', '401', '404', '417']) {
      assert.ok(item.put.responses[status], `PUT ${path} missing response ${status}`);
    }

    assert.ok(item.get.responses['401'], `GET ${path} missing response 401`);
    assert.ok(item.get.responses['404'], `GET ${path} missing response 404`);
  }
});

// FEAT-018: the API entrypoint submenu names the user management and the capture
// endpoints, so the document must carry them. The capture endpoints authenticate
// with the apikey form field, not the admin token.
const EXTRA_PATHS = {
  '/api/admin/user-add': ['post'],
  '/api/admin/user-remove': ['post'],
  '/api/call-upload': ['post'],
  '/api/trunk-recorder-call-upload': ['post'],
};

test('the user management and capture endpoints are documented', () => {
  for (const [path, methods] of Object.entries(EXTRA_PATHS)) {
    for (const method of methods) {
      assert.ok(doc.paths?.[path]?.[method], `missing ${method.toUpperCase()} ${path}`);
    }
  }
});

test('raw-JWT bearer-less security scheme is declared and applied', () => {
  const scheme = doc.components?.securitySchemes?.adminJwt;

  assert.equal(scheme?.type, 'apiKey');
  assert.equal(scheme?.in, 'header');
  assert.equal(scheme?.name, 'Authorization');

  // login works without a token; the capture endpoints take the apikey form field.
  const NO_TOKEN = ['/api/admin/login', '/api/call-upload', '/api/trunk-recorder-call-upload'];

  for (const [path, item] of Object.entries(doc.paths ?? {})) {
    if (NO_TOKEN.includes(path)) {
      continue;
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
