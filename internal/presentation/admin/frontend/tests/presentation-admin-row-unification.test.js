// Row layout unification: the show-code and copy-key icon
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

// the code field rides two controls: the one that shows the code, and the one that draws a new one. They
// share the field's inline row, and neither one is a control the row puts somewhere else.
test('the show-code and generate-code buttons sit inline right of the code input', () => {
  seed({ access: [{ code: 'abc123', ident: 'Primary', systems: '*', order: 1 }] });

  const el = section('rdio-admin-access');

  const inlines = findAll(el, (node) => node.tag === 'div' && node.className === 'field-inline');

  assert.equal(inlines.length, 1, 'the code field has no inline row');

  const input = findAll(inlines[0], (node) => node.tag === 'input')[0];
  const buttons = findAll(inlines[0], (node) => node.tag === 'button');

  assert.equal(input.type, 'password', 'the code field must start hidden');
  assert.equal(buttons.length, 2, 'the code field does not ride both the control that shows its code and the control that draws a new one');

  for (const button of buttons) {
    assert.ok(button.className.includes('rdio-icon-button'), 'a control riding the code field is not an icon button');
    assert.ok(button.getAttribute('aria-label'), 'a control riding the code field announces no name');
    assert.equal(button.type, 'button', 'a control riding the code field submits the section it rides in');
  }
});

// the glyphs the code field is allowed to draw: the pair the show/hide control rides, and the one the
// control that draws a new code rides. A word the icon font does not know renders as that word.
const ACCESS_ICON_WORDS = ['autorenew', 'visibility', 'visibility_off'];

test('every icon riding the code field is a ligature the icon font knows', () => {
  seed({ access: [{ code: 'abc123', ident: 'Primary', systems: '*', order: 1 }] });

  const inline = findAll(section('rdio-admin-access'), (node) => node.tag === 'div' && node.className === 'field-inline')[0];

  const icons = findAll(inline, (node) => node.className === 'material-icons');

  assert.ok(icons.length >= 2, 'the code field rides no icons under its controls');

  for (const icon of icons) {
    assert.ok(ACCESS_ICON_WORDS.includes(icon.textContent), `the code field shows ${icon.textContent}, not a Material Icons ligature`);
    assert.equal(icon.getAttribute('aria-hidden'), 'true', `the ${icon.textContent} mark is read out as text, which says a glyph's shape and not its meaning`);
  }
});

// the two controls riding one field are told apart by sight as well as by name: the draw is the renewal
// glyph, not another eye.
test('the control that draws a new access code carries the autorenew ligature', () => {
  seed({ access: [{ code: 'abc123', ident: 'Primary', systems: '*', order: 1 }] });

  const inline = findAll(section('rdio-admin-access'), (node) => node.tag === 'div' && node.className === 'field-inline')[0];

  const draw = findAll(inline, (node) => node.tag === 'button' && node.getAttribute('aria-label') === 'Generate new code')[0];

  assert.ok(draw, 'the code field carries no control named Generate new code');

  const glyph = findAll(draw, (node) => node.className === 'material-icons')[0];

  assert.ok(glyph, 'the control that draws a new code carries no icon under it');
  assert.equal(glyph.textContent, 'autorenew', `the control that draws a new code shows ${glyph.textContent}, not the renewal glyph that says it replaces the value`);
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

  // the standard caption block is a p with span.body and span.caption
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

  // the Directory caption is a node list; an array in textContent rendered [object Text]
  const captions = findAll(el, (node) => node.tag === 'span' && node.className === 'caption');

  const directory = captions.find((node) => node.textContent.includes('Path of the'));

  assert.ok(directory, 'the Directory caption is missing');
  assert.ok(!directory.textContent.includes('[object'), 'the caption renders node objects as literal text');
});
