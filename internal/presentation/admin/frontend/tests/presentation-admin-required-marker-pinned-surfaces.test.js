// The surfaces the mark is not for: the login box, the OpenAPI box,
// the Tools file forms, the Logs filters, and the systems-selection
// dialog. The OpenAPI asterisk is the contract speaking, not the webapp.
// see the required-marker-pinned-surfaces maintainer note.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-login.js';
import '../presentation/admin/rdio-admin-logs.js';
import '../presentation/admin/rdio-admin-openapi.js';
import '../presentation/admin/rdio-admin-tools.js';
import '../presentation/admin/rdio-systems-selection.js';

import { connectFields, findAll, requiredMark, texts } from './dom-shim.js';

import { buildOpenApiGroups } from '../domain/openapi-view.js';

const contract = JSON.parse(readFileSync(new URL('../../../../../docs/openapi.json', import.meta.url), 'utf8'));

function slice(working) {
  return { baseline: structuredClone(working), working: structuredClone(working), dirty: false, invalid: false, conflict: false };
}

function seed(store) {
  const state = { auth: { authenticated: true, docker: false, passwordNeedChange: false }, alerts: { fire: {} } };

  for (const name of ['access', 'apikeys', 'dirwatch', 'downstreams', 'groups', 'logs', 'options', 'systems', 'tags']) {
    state[`section:${name}`] = slice(name === 'options' ? {} : []);
  }

  for (const level of ['sites', 'talkgroups', 'units']) {
    state[`subsection:${level}:1`] = slice([]);
  }

  store.setState(state);

  return store;
}

function attach(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

function spanClass(node) {
  return node.className?.split(/\s+/).find((name) => name === 'field-title' || name === 'body');
}

// The elements that name a field: the label inside a form field or a caption span.
function namedElements(root) {
  const found = [];

  for (const node of findAll(root, (child) => child.tag === 'rdio-form-field')) {
    const label = findAll(node, (child) => child.tag === 'label')[0];

    if (label) found.push(label);
  }

  for (const span of findAll(root, (child) => spanClass(child))) found.push(span);

  return found;
}

function markedNames(root) {
  return [...new Set(namedElements(root).filter((el) => requiredMark(el)).map((el) => el.textContent))].sort();
}

function controls(root) {
  return findAll(root, (node) => ['input', 'textarea', 'select', 'rdio-select'].includes(node.tag));
}

// ---------------------------------------------------------------------------
// The login box: the password the operator types to reach the server.
// ---------------------------------------------------------------------------

test('the login password field carries no mark and announces nothing required', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-login');

  assert.deepEqual(markedNames(el), [], 'the login box marks no name, the way the legacy form hid the marker there');

  for (const control of controls(el)) {
    assert.equal(control.getAttribute('aria-required'), null, 'the login control announces nothing required');
  }
});

// ---------------------------------------------------------------------------
// The OpenAPI box: the asterisk its own contract states, and no other.
// ---------------------------------------------------------------------------

async function openApiCard() {
  seed(getAdminContext().store);

  globalThis.fetch = async () => ({ ok: true, json: async () => contract });

  const el = new (customElements.get('rdio-admin-openapi'))();

  await el.connectedCallback();

  delete globalThis.fetch;

  const boxes = findAll(el, (node) => node.tag === 'rdio-admin-openapi-box');

  for (const box of boxes) box.connectedCallback();

  return el;
}

test('the OpenAPI box marks exactly the fields its contract says are required', async () => {
  const el = await openApiCard();

  const names = findAll(el, (node) => node.className?.split(/\s+/).includes('field-name'));

  assert.ok(names.length > 0, 'the box names the fields of the request bodies it renders');

  const marked = [...new Set(names.filter((el2) => requiredMark(el2)).map((node) => node.textContent))].sort();

  const endpoints = buildOpenApiGroups(contract)
    .flatMap((group) => group.endpoints ?? group.subgroups?.flatMap((subgroup) => subgroup.endpoints) ?? []);

  const required = [...new Set(endpoints
    .flatMap((endpoint) => endpoint.body ?? [])
    .flatMap((body) => body.fields ?? [])
    .filter((field) => field.required)
    .map((field) => field.name))].sort();

  assert.deepEqual(marked, required, 'the asterisk on a documented field is the contract stating the field is required, and the webapp adds none');
});

// ---------------------------------------------------------------------------
// The Tools file forms, the Logs filters, and the systems-selection dialog.
// ---------------------------------------------------------------------------

test('the Tools file forms carry no mark', () => {
  seed(getAdminContext().store);

  const tools = attach('rdio-admin-tools');

  const filePanels = findAll(tools, (node) => node.className?.split(/\s+/).includes('rdio-admin-row'))
    .filter((panel) => texts(findAll(panel, (child) => child.className?.split(/\s+/).includes('row-title')))[0] !== 'Change password');

  assert.ok(filePanels.length >= 3, 'the export, import, and CSV import panels are on the page');

  for (const panel of filePanels) {
    assert.deepEqual(markedNames(panel), [], `${texts(findAll(panel, (child) => child.className?.split(/\s+/).includes('row-title')))[0]} marks no name`);
  }
});

test('the Logs filters carry no mark and announce nothing required', () => {
  seed(getAdminContext().store);

  const el = attach('rdio-admin-logs');

  assert.deepEqual(markedNames(el), [], 'the filters that narrow a list mark no name');

  for (const control of controls(el)) {
    assert.equal(control.getAttribute('aria-required'), null, 'no filter announces itself required');
  }
});

test('the systems-selection dialog carries no mark', () => {
  seed(getAdminContext().store);

  const dialog = new (customElements.get('rdio-systems-selection'))();

  dialog.setup({
    groups: [{ id: 1, label: 'Dispatch' }],
    tags: [{ id: 3, label: 'Fire' }],
    systems: [{ systemRef: 1, label: 'Metro', talkgroups: [{ talkgroupRef: 70, label: 'Fire dispatch', groupIds: [1] }] }],
  }, '*');

  connectFields(dialog);

  assert.deepEqual(markedNames(dialog), [], 'the dialog that picks which systems a row allows marks no name');

  for (const control of controls(dialog)) {
    assert.equal(control.getAttribute('aria-required'), null, 'no part of the dialog announces itself required');
  }
});
