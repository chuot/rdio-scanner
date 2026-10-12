// The per-level sections: each section shows the
// saved systems in display order in its dropdown, edits the rows of the selected
// system in its own slice, and the talkgroups section carries the Blacklist
// action that appends the talkgroup id to the system's blacklists.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-sites.js';
import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-units.js';

import { findAll, texts } from './dom-shim.js';

function systems() {
  return [
    {
      id: 2,
      label: 'Harbour',
      order: 1,
      blacklists: '7',
      sites: [{ id: 5, label: 'East', siteRef: 77, order: 1 }],
      talkgroups: [{ id: 10, label: 'Ground', talkgroupRef: 1514, order: 1, groupIds: [], tagId: null }],
      units: [{ id: 20, label: 'Car 1', unitRef: 1001, order: 1 }],
    },
    {
      id: 1,
      label: 'Metro',
      order: 2,
      blacklists: '',
      sites: [],
      talkgroups: [],
      units: [],
    },
  ];
}

function seed() {
  const { store } = getAdminContext();

  const state = { auth: { authenticated: true, docker: false, passwordNeedChange: false } };

  for (const level of ['sites', 'talkgroups', 'units']) {
    for (const system of systems()) {
      state[`subsection:${level}:${system.id}`] = {
        baseline: structuredClone(system[level]),
        working: structuredClone(system[level]),
        dirty: false,
        invalid: false,
        conflict: false,
      };
    }
  }

  store.setState({
    ...state,
    'section:systems': { baseline: structuredClone(systems()), working: systems(), dirty: false, invalid: false, conflict: false },
    'section:groups': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:tags': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    alerts: {},
  });

  return store;
}

function section(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();

  return el;
}

function dropdown(el) {
  return findAll(el, (node) => node.tag === 'rdio-select')[0];
}

function rowTitles(el) {
  return texts(findAll(el, (node) => node.className === 'row-title'));
}

function addButton(el) {
  return findAll(el, (node) => node.className.split(/\s+/).includes('add'))[0];
}

test('the sites section lists the saved systems in display order and shows the rows of the selected one', () => {
  seed();

  const el = section('rdio-admin-sites');

  const options = JSON.parse(dropdown(el).getAttribute('options'));

  assert.deepEqual(options, [['2', 'Harbour'], ['1', 'Metro']]);
  assert.equal(dropdown(el).getAttribute('value'), '2');

  assert.deepEqual(rowTitles(el), ['East']);
});

test('adding a site puts a new row on top and renumbers the order', () => {
  const store = seed();

  const el = section('rdio-admin-sites');

  addButton(el).click();

  const working = store.getState()['subsection:sites:2'].working;

  assert.equal(working.length, 2);
  assert.equal(working[0].id, 0);
  assert.equal(working[0].order, 0);
  assert.equal(working[1].label, 'East');
  assert.equal(working[1].order, 2);
});

test('the units section shows the unit rows of the selected system', () => {
  seed();

  const el = section('rdio-admin-units');

  assert.deepEqual(rowTitles(el), ['Car 1']);
});

test('blacklisting a talkgroup appends its id to the system blacklists and removes the row', () => {
  const store = seed();

  const el = section('rdio-admin-talkgroups');

  const blacklist = findAll(el, (node) => node.tag === 'button' && node.textContent === 'Blacklist talkgroup')[0];

  assert.ok(blacklist, 'the talkgroups section has no Blacklist action');

  blacklist.click();

  const systems = store.getState()['section:systems'].working;

  assert.equal(systems.find((s) => s.id === 2).blacklists, '7,10');
  assert.equal(store.getState()['subsection:talkgroups:2'].working.length, 0);
});
