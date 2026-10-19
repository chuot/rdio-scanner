// The OpenAPI document docs/openapi.json is the contract the front ends and the command-line
// tooling are written against. It must parse with Node's built-in JSON support and cover every
// entry point the server answers: the admin REST surface, the two ingestion endpoints, the two
// WebSocket surfaces, and the two static applications.

import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

import { fileURLToPath } from 'node:url';

const DOC_PATH = fileURLToPath(new URL('../../../../../docs/openapi.json', import.meta.url));
const VERSION_PATH = fileURLToPath(new URL('../../../../../VERSION.txt', import.meta.url));

const doc = JSON.parse(readFileSync(DOC_PATH, 'utf8'));

const SECTIONS = ['access', 'apikeys', 'dirwatch', 'downstreams', 'groups', 'options', 'systems', 'tags'];

test('the OpenAPI document declares OpenAPI 3.1 with a title and the server version', () => {
  assert.equal(doc.openapi, '3.1.0');
  assert.ok(doc.info?.title);

  const version = readFileSync(VERSION_PATH, 'utf8').trim();

  assert.equal(doc.info.version, version, 'info.version must equal VERSION.txt');
});

test('every legacy admin endpoint is documented', () => {
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

// The Systems section saves the system row: its own fields, its blacklist included. The sites,
// talkgroups, and units under a system are saved by the level that shows them, so the section's
// body names the row and its answer carries the levels as the store holds them.
test('the systems section body is the system row, and its answer is the system with its levels', () => {
  const item = doc.paths?.['/api/admin/config/{section}'];

  const submitted = item.put.requestBody.content['application/json'].schema.oneOf;
  const answered = item.put.responses['200'].content['application/json'].schema.oneOf;

  assert.ok(submitted.some((branch) => branch.items?.$ref === '#/components/schemas/SystemRow'),
    'the systems section body does not name the system row');

  assert.ok(answered.some((branch) => branch.items?.$ref === '#/components/schemas/System'),
    'the systems section answer does not name the system with what is stored under it');

  const row = doc.components?.schemas?.SystemRow;

  assert.ok(row?.properties?.blacklists, 'the system row carries no blacklist to save');

  for (const level of ['sites', 'talkgroups', 'units']) {
    assert.ok(!row?.properties?.[level], `the system row carries the ${level} that the ${level} section saves`);
    assert.ok(doc.components?.schemas?.System?.properties?.[level],
      `the system answer carries no ${level} read back from the store`);
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

    // The body is the list of the level's rows. The talkgroups level takes one shape more: the
    // object that names the rows beside the system's whole blacklist, so the save that drops a
    // talkgroup stores the entry that refuses it in the same request.
    const branches = body.oneOf ?? [body];
    const list = branches.find((branch) => branch.items?.$ref === `#/components/schemas/${schema}`);

    assert.ok(list, `PUT ${path} body does not reference ${schema}`);

    if (level === 'talkgroups') {
      const carrier = branches.find((branch) => branch.$ref === '#/components/schemas/TalkgroupsSave');

      assert.ok(carrier, `PUT ${path} body does not name the object carrying the blacklist`);

      const save = doc.components?.schemas?.TalkgroupsSave;

      assert.deepEqual(save?.required, ['talkgroups'], 'the object body does not require the rows it carries');
      assert.equal(save?.properties?.talkgroups?.items?.$ref, '#/components/schemas/Talkgroup',
        'the object body does not carry the talkgroups as the level carries them');
      assert.equal(save?.properties?.blacklists?.type, 'string',
        'the object body does not carry the blacklist as the text the system row uses');
      assert.equal(branches.length, 2, 'the talkgroups body names more shapes than the list of rows and the object');
    } else {
      assert.equal(branches.length, 1, `PUT ${path} body names more shapes than the list of its rows`);
    }

    for (const status of ['400', '401', '404', '417']) {
      assert.ok(item.put.responses[status], `PUT ${path} missing response ${status}`);
    }

    assert.ok(item.get.responses['401'], `GET ${path} missing response 401`);
    assert.ok(item.get.responses['404'], `GET ${path} missing response 404`);
  }
});

// The API entrypoint submenu names the user management and the capture
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

// The status read is the only boundary in the server that answers what the server is doing right now,
// and the administrator's landing page is built out of it alone. It answers GET and nothing else: it
// reads, and a page that could write through it would be a page that could stop the server.
test('the status read is documented as the one read that answers the whole snapshot', () => {
  const item = doc.paths?.['/api/admin/status'];

  assert.ok(item?.get, 'missing GET /api/admin/status');

  for (const method of ['post', 'put', 'patch', 'delete']) {
    assert.ok(!item[method], `the status read must not document ${method.toUpperCase()}: it reads and never writes`);
  }

  assert.deepEqual(item.get.security, [{ adminJwt: [] }], 'the status read is not open to an anonymous caller');

  const answered = item.get.responses['200'].content['application/json'].schema;

  assert.equal(answered.$ref, '#/components/schemas/AdminStatus',
    'the status read does not answer the status snapshot');

  const status = doc.components?.schemas?.AdminStatus;

  assert.ok(status, 'missing the AdminStatus schema');

  for (const block of ['server', 'live', 'activity', 'logs']) {
    assert.ok(status.properties?.[block], `the status snapshot carries no ${block} block`);
  }

  // The lists are lists even when the server has recorded nothing, because a page that has to check
  // each of them for being absent is a page that will eventually stop checking.
  for (const list of ['hours', 'busiest', 'recent']) {
    assert.equal(status.properties?.activity?.properties?.[list]?.type, 'array',
      `activity.${list} is not a list`);
  }

  assert.equal(status.properties?.logs?.properties?.sources?.type, 'array', 'logs.sources is not a list');

  // The page already holds the whole configuration, so the counts that come out of it are derived
  // there; the status read carries no configuration of any kind.
  for (const absent of ['config', 'options', 'systems', 'dirwatch', 'downstreams', 'alerts']) {
    assert.ok(!status.properties?.[absent], `the status snapshot carries ${absent}, which is configuration and not status`);
  }

  for (const code of ['401', '405', '417']) {
    assert.ok(item.get.responses[code], `the status read documents no ${code} answer`);
  }

  assert.ok(item.get.responses['401'].$ref || item.get.responses['401'].description,
    'the status read\'s 401 answer says nothing');
});

test('raw-JWT bearer-less security scheme is declared and applied', () => {
  const scheme = doc.components?.securitySchemes?.adminJwt;

  assert.equal(scheme?.type, 'apiKey');
  assert.equal(scheme?.in, 'header');
  assert.equal(scheme?.name, 'Authorization');

  // login works without a token; the capture endpoints take the apikey form field; the
  // static applications are public; the call history takes a listener access code beside the
  // admin JWT. Everything else carries the admin JWT.
  const NO_TOKEN = ['/api/admin/login', '/api/call-upload', '/api/trunk-recorder-call-upload'];
  const PUBLIC = ['/', '/admin', '/admin/'];

  for (const [path, item] of Object.entries(doc.paths ?? {})) {
    for (const [method, operation] of Object.entries(item)) {
      if (!['get', 'put', 'post', 'patch', 'delete'].includes(method)) continue;

      if (NO_TOKEN.includes(path) || PUBLIC.includes(path)) {
        assert.deepEqual(operation.security, [], `${method.toUpperCase()} ${path} must declare no security`);
        continue;
      }

      if (CALL_PATHS[path]) {
        // A listener-facing endpoint answers to two principals with two sets of rights, and the
        // document says so rather than pretending one credential covers both.
        assert.deepEqual(operation.security, [{ adminJwt: [] }, { accessCode: [] }],
          `${method.toUpperCase()} ${path} must accept the access code beside the admin JWT`);
        continue;
      }

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

// The ingestion endpoints are form uploads keyed by an apikey, not by the admin JWT. The
// field catalogue is the parser's own: every field name internal/infrastructure/dirwatch/
// parsers/multipart.go reads must appear in the documented form, and the trunk-recorder
// endpoint adds its JSON sidecar field.
const UPLOAD_FIELDS = [
  'audio', 'audioFilename', 'audioName', 'audioMime', 'audioType', 'channelType',
  'dateTime', 'frequency', 'frequencies', 'key', 'patches', 'patched_talkgroups',
  'site', 'source', 'sources', 'system', 'systemLabel', 'talkgroup', 'talkgroupGroup',
  'talkgroupGroups', 'talkgroupLabel', 'talkgroupName', 'talkgroupTag', 'timestamp',
  'unit', 'units',
];

test('both ingestion endpoints are documented with the parser field catalogue', () => {
  for (const path of ['/api/call-upload', '/api/trunk-recorder-call-upload']) {
    const post = doc.paths?.[path]?.post;

    assert.ok(post, `missing POST ${path}`);

    const schema = post.requestBody?.content?.['multipart/form-data']?.schema;

    assert.equal(schema?.type, 'object', `${path} must document a multipart form object`);

    for (const field of UPLOAD_FIELDS) {
      assert.ok(schema.properties?.[field], `${path} does not document the ${field} field`);
    }
  }

  const sidecar = doc.paths?.['/api/trunk-recorder-call-upload']?.post?.requestBody?.content
    ?.['multipart/form-data']?.schema?.properties?.meta;

  assert.ok(sidecar, 'the trunk-recorder endpoint must document the meta sidecar field');
});

// The listener protocol and the administrator configuration socket are documented as
// x-websocket components: the command set
// mirrors internal/presentation/scanner/backend/message.go.
const LISTENER_COMMANDS = ['CAL', 'CFG', 'IOS', 'LCL', 'LFM', 'LSC', 'MAX', 'PID', 'PIN', 'SRV', 'VER', 'XPR'];

test('the listener WebSocket protocol is documented with its full command set', () => {
  const listener = doc.components?.['x-websockets']?.listener;

  assert.ok(listener, 'components.x-websockets.listener is missing');
  assert.deepEqual(Object.keys(listener.commands).sort(), LISTENER_COMMANDS,
    'the documented command set must mirror message.go');
  assert.ok(listener.path, 'the listener socket must name its path');
});

test('the administrator configuration socket is documented on the whole-config path', () => {
  const adminConfig = doc.components?.['x-websockets']?.adminConfig;

  assert.ok(adminConfig, 'components.x-websockets.adminConfig is missing');
  assert.equal(adminConfig.path, '/api/admin/config');

  assert.ok(doc.paths?.['/api/admin/config']?.['x-websocket'],
    'the whole-config path item must cross-reference the configuration socket');
});

test('the two static applications and the admin redirect are documented', () => {
  assert.ok(doc.paths?.['/']?.get, 'missing GET / (the scanner webapp)');
  assert.ok(doc.paths?.['/admin/']?.get, 'missing GET /admin/ (the admin shell)');

  const redirect = doc.paths?.['/admin']?.get?.responses?.['307'];

  assert.ok(redirect, 'GET /admin must document the 307 redirect to /admin/');
});

// The call history over HTTP. The recorded calls live today on the listener socket, and these three
// endpoints are the same history on the HTTP side of the server, under the same licence. They are
// listener-facing, so they answer to a listener access code, and an administrator session opens the whole
// history through the credential the admin page already presents.
const CALL_PATHS = {
  '/api/calls/search': ['post'],
  '/api/calls/{callId}': ['get'],
  '/api/calls/{callId}/audio': ['get'],
};

const CALL_SCHEMAS = ['CallsSearchRequest', 'CallsSearchResponse', 'CallSummary', 'CallResponse', 'CallFrequency', 'CallUnit'];

test('the three call history endpoints are documented with their methods', () => {
  for (const [path, methods] of Object.entries(CALL_PATHS)) {
    for (const method of methods) {
      assert.ok(doc.paths?.[path]?.[method], `missing ${method.toUpperCase()} ${path}`);
    }
  }
});

test('the listener access code is declared as a security scheme', () => {
  const scheme = doc.components?.securitySchemes?.accessCode;

  assert.ok(scheme, 'components.securitySchemes.accessCode is missing');
  assert.equal(scheme.type, 'apiKey');
  assert.equal(scheme.in, 'header');
  assert.equal(scheme.name, 'X-Access-Code');
});

test('the call history endpoints accept the access code and the administrator session', () => {
  for (const [path, methods] of Object.entries(CALL_PATHS)) {
    for (const method of methods) {
      const operation = doc.paths?.[path]?.[method];

      assert.ok(operation, `missing ${method.toUpperCase()} ${path}`);
      assert.deepEqual(operation.security, [{ adminJwt: [] }, { accessCode: [] }],
        `${method.toUpperCase()} ${path} must accept both credentials`);
    }
  }
});

test('the call history schemas are declared', () => {
  for (const name of CALL_SCHEMAS) {
    assert.ok(doc.components?.schemas?.[name], `missing schema ${name}`);
  }
});

test('the call search documents its request, its page, and its statuses', () => {
  const post = doc.paths?.['/api/calls/search']?.post;

  assert.ok(post, 'missing POST /api/calls/search');

  const body = post.requestBody?.content?.['application/json']?.schema;

  assert.equal(body?.$ref, '#/components/schemas/CallsSearchRequest',
    'the search body must reference CallsSearchRequest');

  const page = post.responses?.['200']?.content?.['application/json']?.schema;

  assert.equal(page?.$ref, '#/components/schemas/CallsSearchResponse',
    'the search answer must reference CallsSearchResponse');

  const rows = doc.components?.schemas?.CallsSearchResponse?.properties?.results;

  assert.equal(rows?.items?.$ref, '#/components/schemas/CallSummary',
    'the page rows must reference CallSummary');

  for (const status of ['200', '400', '401', '405', '417']) {
    assert.ok(post.responses?.[status], `POST /api/calls/search missing response ${status}`);
  }
});

test('one call is documented as metadata, with the recording named and not carried', () => {
  const get = doc.paths?.['/api/calls/{callId}']?.get;

  assert.ok(get, 'missing GET /api/calls/{callId}');

  const param = get.parameters?.find((p) => p.name === 'callId');

  assert.ok(param?.required, 'GET /api/calls/{callId} missing the required callId path parameter');

  const call = get.responses?.['200']?.content?.['application/json']?.schema;

  assert.equal(call?.$ref, '#/components/schemas/CallResponse',
    'the call answer must reference CallResponse');

  for (const status of ['200', '401', '404', '405']) {
    assert.ok(get.responses?.[status], `GET /api/calls/{callId} missing response ${status}`);
  }

  const properties = doc.components?.schemas?.CallResponse?.properties;

  assert.ok(properties, 'CallResponse declares no properties');

  // The recording is its own entry point, so a metadata answer names the recording's bytes and
  // carries none of them: a metadata question is asked far more often than a recording is listened
  // to, and a metadata answer carrying the recording makes every metadata answer a download.
  assert.ok(!('audio' in properties), 'CallResponse must not carry the recording bytes');

  for (const field of ['audioName', 'audioType', 'audioLength', 'audioUrl']) {
    assert.ok(properties[field], `CallResponse does not document the ${field} field`);
  }

  const refs = JSON.stringify(doc.components?.schemas?.CallResponse);

  assert.ok(refs.includes('CallFrequency'), 'CallResponse does not reference CallFrequency');
  assert.ok(refs.includes('CallUnit'), 'CallResponse does not reference CallUnit');
});

test('the recording is documented as its own entry point, range requests included', () => {
  const get = doc.paths?.['/api/calls/{callId}/audio']?.get;

  assert.ok(get, 'missing GET /api/calls/{callId}/audio');

  const param = get.parameters?.find((p) => p.name === 'callId');

  assert.ok(param?.required, 'GET /api/calls/{callId}/audio missing the required callId path parameter');

  for (const status of ['200', '206', '401', '404', '405']) {
    assert.ok(get.responses?.[status], `GET /api/calls/{callId}/audio missing response ${status}`);
  }

  const recording = get.responses?.['200']?.content;

  assert.ok(recording?.['application/octet-stream'] || recording?.['audio/*'],
    'the recording answer must document binary audio content');

  // The name the recording was stored under, on the wire, so a client saving it names the file the
  // server recorded.
  const disposition = get.responses?.['200']?.headers?.['Content-Disposition'];

  assert.ok(disposition, 'the recording answer must document Content-Disposition');
});
