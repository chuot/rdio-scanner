// The Ingestion section (FEAT-018): an informational section explaining the three
// ways calls reach the server. The entrypoint list lives in the API section; this
// card has no OpenAPI box, no save bar, and no store slice: nothing here is edited.

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

test('the Ingestion section has no save bar and renders the ingestion help box', () => {
  const el = ingestionSection();

  assert.equal(findAll(el, (node) => node.tag === 'rdio-admin-section-bar').length, 0, 'an informational section must not offer a save bar');

  // The help box below the three panels is the ingestion group of the shared
  // OpenAPI box, the same renderer the API section uses.
  const boxes = findAll(el, (node) => node.tag === 'rdio-admin-openapi-box');

  assert.equal(boxes.length, 1, 'the section does not render the ingestion help box');
  assert.equal(boxes[0].dataset.group, 'ingestion', 'the help box is not the ingestion group');
});
