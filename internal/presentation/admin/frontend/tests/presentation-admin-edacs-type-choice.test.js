// the type the administration offers beside a system and beside a talkgroup,
// and the words that caption that choice: the type is what decides the form a
// listener reads a talkgroup number in, so it has to be offered where the
// operator makes it, in the shape the other types are offered in.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-systems.js';
import '../presentation/admin/rdio-admin-talkgroups.js';

import { connectFields, findAll } from './dom-shim.js';

const METRO = { id: 1, label: 'Metro', order: 1, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };
const TALKGROUP = { id: 7, label: 'Fire', name: 'Fire dispatch', talkgroupRef: 70, order: 1, groupIds: [], tagId: null, alert: '', delay: null, frequency: null, led: '', type: '' };

function slice(working) {
  return { baseline: structuredClone(working), working: structuredClone(working), dirty: false, invalid: false, conflict: false };
}

function seed(level, rows) {
  const state = {
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    alerts: {},
    'section:systems': slice([METRO]),
    'section:groups': slice([]),
    'section:tags': slice([]),
  };

  state[`subsection:${level}:${METRO.id}`] = slice(rows);

  getAdminContext().store.setState(state);

  return getAdminContext().store;
}

function attach(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

// the row the section opens for the operator to fill, the way the page's own
// add button opens it.
function addRow(el) {
  findAll(el, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes('add'))[0].click();
  connectFields(el);

  return findAll(el, (node) => node.tag === 'details')[0];
}

// the field a row names by a word beside its control: the label the field
// carries, and beside it the caption block with its legend.
function fieldFor(row, title) {
  return findAll(row, (node) =>
    node.tag === 'rdio-form-field' && findAll(node, (child) => child.tag === 'label' && child.textContent.trim() === title)[0],
  )[0] ?? null;
}

function offeredChoices(row, title) {
  const field = fieldFor(row, title);

  assert.ok(field, `the row has no ${title} field`);

  return findAll(field, (node) => node.tag === 'option')
    .map((option) => ({ text: option.textContent, value: option.value ?? option.attributes.value }));
}

function legendOf(row, title) {
  const field = fieldFor(row, title);

  assert.ok(field, `the row has no ${title} field`);

  return findAll(field.parent, (node) => node.tag === 'li').map((item) => ({
    name: findAll(item, (child) => child.tag === 'b')[0]?.textContent,
    text: item.textContent,
  }));
}

test('the system form offers EDACS as a type, and its caption legend names it', () => {
  seed('systems', [METRO]);

  const row = addRow(attach('rdio-admin-systems'));

  const offered = offeredChoices(row, 'Type').find((choice) => choice.value === 'edacs');

  assert.ok(offered, 'the system Type control offers no EDACS choice');
  assert.equal(offered.text, 'EDACS', 'the EDACS choice is offered under another name');

  const legend = legendOf(row, 'Type');
  const item = legend.find((entry) => entry.name === 'EDACS');

  assert.ok(item, 'the system Type caption legend says nothing about EDACS');

  const [pairName, ...description] = item.text.split(' - ');

  assert.equal(pairName, 'EDACS', 'the EDACS legend entry does not name the type the way the others do');
  assert.ok(description.join(' - ').trim().length > 0, 'the EDACS legend entry names the type without describing it');
  assert.match(item.text, /\.$/, 'the EDACS legend entry is not a sentence like the ones beside it');

  assert.deepEqual(legend.map((entry) => entry.name), offeredChoices(row, 'Type').filter((choice) => choice.value !== '').map((choice) => choice.text),
    'the system Type caption legend and the choices the control offers no longer name the same types');
});

test('the talkgroup form offers EDACS as a type, and its caption legend names it', () => {
  seed('talkgroups', [TALKGROUP]);

  const row = addRow(attach('rdio-admin-talkgroups'));

  const offered = offeredChoices(row, 'Type').find((choice) => choice.value === 'edacs');

  assert.ok(offered, 'the talkgroup Type control offers no EDACS choice');
  assert.equal(offered.text, 'EDACS', 'the EDACS choice is offered under another name');

  const legend = legendOf(row, 'Type');
  const item = legend.find((entry) => entry.name === 'EDACS');

  assert.ok(item, 'the talkgroup Type caption legend says nothing about EDACS');
  assert.match(item.text, /\.$/, 'the EDACS legend entry is not a sentence like the ones beside it');

  assert.deepEqual(legend.map((entry) => entry.name), offeredChoices(row, 'Type').filter((choice) => choice.value !== '').map((choice) => choice.text),
    'the talkgroup Type caption legend and the choices the control offers no longer name the same types');
});

test('the EDACS type the operator chooses is the type the system row carries to be saved', () => {
  const store = seed('systems', [METRO]);

  const row = addRow(attach('rdio-admin-systems'));

  const offered = offeredChoices(row, 'Type').find((choice) => choice.value === 'edacs');

  assert.ok(offered, 'the system Type control offers no EDACS choice for the operator to pick');

  findAll(fieldFor(row, 'Type'), (node) => node.tag === 'rdio-select')[0]
    .dispatchEvent(new CustomEvent('change', { detail: offered.value }));

  assert.equal(store.getState()['section:systems'].working[0].type, 'edacs',
    'the system row does not carry the type the operator chose');
});

test('the talkgroup id field still asks for the number in decimal and holds a number', () => {
  seed('talkgroups', [TALKGROUP]);

  const row = addRow(attach('rdio-admin-talkgroups'));

  const field = fieldFor(row, 'Id');

  assert.ok(field, 'the talkgroup row has no Id field');

  const caption = findAll(field.parent, (node) => node.className?.split(/\s+/).includes('caption'))[0];

  assert.equal(caption?.textContent, 'Talkgroup identifier in decimal format.',
    'the talkgroup id field no longer says the number it wants is decimal');
  assert.equal(findAll(field, (node) => node.tag === 'input')[0]?.type, 'number',
    'the talkgroup id field no longer holds a number');
});
