// The Ingestion section (T020, CA-018): an informational section explaining the
// three ways calls reach the server, each naming its OpenAPI entry point, with
// the help box at the bottom and a nav item in the shell. It has no save bar
// and no store slice: nothing here is edited.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/openapi-box.js';
import '../presentation/admin/rdio-admin-ingestion.js';

import { findAll, texts } from './dom-shim.js';

function ingestionSection() {
  const { store } = getAdminContext();

  store.setState({ auth: { authenticated: true, docker: false, passwordNeedChange: false }, alerts: {} });

  const el = new (customElements.get('rdio-admin-ingestion'))();

  el.connectedCallback();

  return el;
}

function sectionText(el) {
  return texts(findAll(el, (node) => node.tag === 'p')).join(' ');
}

test('the Ingestion section explains the three ingestion ways', () => {
  const el = ingestionSection();

  const titles = texts(findAll(el, (node) => node.className === 'row-title'));

  assert.equal(titles.length, 3, 'the section does not present three ingestion methods');

  const text = sectionText(el);

  assert.ok(text.includes('POST /api/call-upload'), 'the section does not name the capture endpoint');
  assert.ok(text.includes('apikey'), 'the section does not explain the apikey capture path');
  assert.ok(text.includes('Dirwatch'), 'the section does not explain dirwatch ingestion');
  assert.ok(text.includes('Downstreams'), 'the section does not explain downstream ingestion');
});

test('the Ingestion section ends with the OpenAPI box naming the capture endpoint', () => {
  const el = ingestionSection();

  const boxes = findAll(el, (node) => node.tag === 'rdio-admin-openapi-box');

  assert.equal(boxes.length, 1, 'the section card does not end with the OpenAPI box');

  boxes[0].connectedCallback();

  const endpoints = texts(findAll(boxes[0], (node) => node.tag === 'code'));

  assert.ok(endpoints.includes('POST /api/call-upload'), 'the box does not name the capture endpoint');

  const example = findAll(boxes[0], (node) => node.tag === 'pre')[0];

  assert.ok(example.textContent.includes('curl -F'), 'the box example is not the multipart capture call');
});

test('the Ingestion section has no save bar', () => {
  const el = ingestionSection();

  assert.equal(findAll(el, (node) => node.tag === 'rdio-admin-section-bar').length, 0, 'an informational section must not offer a save bar');
});
