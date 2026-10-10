// Row layout unification (FEAT-019, CA-006): the show-code and copy-key icon
// buttons sit inline right of their field, Options carries the intro caption every
// other section has, and Dirwatch renders its help through the standard caption
// block (span.body + span.caption) instead of its own builder.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-access.js';
import '../presentation/admin/rdio-admin-apikeys.js';
import '../presentation/admin/rdio-admin-dirwatch.js';
import '../presentation/admin/rdio-admin-options.js';

import { findAll } from './dom-shim.js';

function seed(rows) {
  const { store } = getAdminContext();

  const state = { auth: { authenticated: true, docker: false, passwordNeedChange: false }, alerts: {} };

  for (const name of ['access', 'apikeys', 'dirwatch', 'options']) {
    state[`section:${name}`] = {
      baseline: rows[name] ?? (name === 'options' ? {} : []),
      working: rows[name] ?? (name === 'options' ? {} : []),
      dirty: false,
      invalid: false,
      conflict: false,
    };
  }

  store.setState(state);
}

function section(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();

  return el;
}

test('the show-code button sits inline right of the code input', () => {
  seed({ access: [{ code: 'abc123', ident: 'Primary', systems: '*', order: 1 }] });

  const el = section('rdio-admin-access');

  const inlines = findAll(el, (node) => node.tag === 'div' && node.className === 'field-inline');

  assert.equal(inlines.length, 1, 'the code field has no inline row');

  const input = findAll(inlines[0], (node) => node.tag === 'input')[0];
  const button = findAll(inlines[0], (node) => node.tag === 'button')[0];

  assert.equal(input.type, 'password', 'the code field must start hidden');
  assert.ok(button.className.includes('rdio-icon-button'), 'the inline row has no icon button');
});

test('the copy-key button sits inline right of the key input', () => {
  seed({ apikeys: [{ key: 'k1', ident: 'Harness', systems: '*', order: 1 }] });

  const el = section('rdio-admin-apikeys');

  const inlines = findAll(el, (node) => node.tag === 'div' && node.className === 'field-inline');

  assert.equal(inlines.length, 1, 'the key field has no inline row');

  const input = findAll(inlines[0], (node) => node.tag === 'input')[0];
  const button = findAll(inlines[0], (node) => node.tag === 'button')[0];

  assert.equal(input.value, 'k1', 'the inline row does not carry the key input');
  assert.ok(button.className.includes('rdio-icon-button'), 'the inline row has no icon button');
});

test('Options opens with an intro caption like every other section', () => {
  seed({});

  const el = section('rdio-admin-options');

  const captions = findAll(el, (node) => node.tag === 'p' && node.className === 'caption');

  assert.equal(captions.length, 1, 'the Options card has no intro caption');
  assert.ok(captions[0].textContent.includes('options'), 'the intro caption does not name what the section controls');
});

test('Dirwatch renders its help through the standard caption block', () => {
  seed({ dirwatch: [{ type: 'default', directory: '/var/spool', extension: 'wav', disabled: false }] });

  const el = section('rdio-admin-dirwatch');

  // The standard caption block is a p with span.body and span.caption; the retired
  // dirwatch builder used span.row-title and span.row-caption.
  const bodies = findAll(el, (node) => node.tag === 'span' && node.className === 'body');
  const captions = findAll(el, (node) => node.tag === 'span' && node.className === 'caption');

  assert.ok(bodies.length >= 2, 'the dirwatch rows do not use the standard caption block');
  assert.ok(captions.length >= 2, 'the dirwatch rows do not carry standard captions');

  const legacy = findAll(el, (node) => node.tag === 'span' && node.className === 'row-caption');

  assert.equal(legacy.length, 0, 'the dirwatch rows still carry the retired caption variant');
});

test('the dirwatch Directory caption renders its emphasized tokens as text', () => {
  seed({ dirwatch: [{ type: 'default', directory: '/var/spool', extension: 'wav', disabled: false }] });

  const el = section('rdio-admin-dirwatch');

  // The Directory caption is a list of nodes (text plus bold token names); assigning
  // an array to textContent rendered "[object Text]" literals in the browser.
  const captions = findAll(el, (node) => node.tag === 'span' && node.className === 'caption');

  const directory = captions.find((node) => node.textContent.includes('Path of the'));

  assert.ok(directory, 'the Directory caption is missing');
  assert.ok(!directory.textContent.includes('[object'), 'the caption renders node objects as literal text');
});
