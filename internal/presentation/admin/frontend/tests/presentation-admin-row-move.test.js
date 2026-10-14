// The keyboard's route to a row's position in a drag-sorted section.
//
// The sections of the systems tree are ordered by the operator: the order is what the listener shows,
// and the order is what the server keeps. The pointer's route is the drag handle — a gesture a
// keyboard does not have, and a gesture an operator who cannot keep a pointer steady on a moving row
// cannot use. The row's position needs a control the keyboard can stop on: two buttons that move the
// row one place up or down, going through the same reorder the drag goes through and saved by the same
// save bar the drag is saved by.
//
// The readings are taken on the rendered rows and on the store's working copy under them: the page the
// operator reads, and the state the save bar sends. The buttons are answered through the click event a
// mouse and the keyboard's Enter and Space both leave on a button.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-systems.js';
import '../presentation/admin/rdio-admin-sites.js';
import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-units.js';
import '../presentation/admin/rdio-admin-groups.js';
import '../presentation/admin/rdio-admin-tags.js';

import { reorderAfterDrag, sortedByOrder } from '../presentation/admin/section-shell.js';

import { connectFields, findAll } from './dom-shim.js';

const METRO = { id: 1, label: 'Metro', order: 2, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };
const COUNTY = { id: 2, label: 'County', order: 1, systemRef: 2, blacklists: '', sites: [], talkgroups: [], units: [] };
const DEPOT = { id: 3, label: 'Depot', order: 3, systemRef: 3, blacklists: '', sites: [], talkgroups: [], units: [] };

const GROUPS = [
  { id: 1, label: 'Dispatch', order: 1 },
  { id: 2, label: 'Interop', order: 2 },
  { id: 3, label: 'Console', order: 3 },
];

const TAGS = [{ id: 10, label: 'Fire', order: 1 }];

function slice(baseline) {
  return { baseline: structuredClone(baseline), working: structuredClone(baseline), dirty: false, invalid: false, conflict: false };
}

function seed() {
  getAdminContext().store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    alerts: {},
    'section:systems': slice([METRO, COUNTY, DEPOT]),
    'section:groups': slice(GROUPS),
    'section:tags': slice(TAGS),
  });
}

function mount(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

// The rows the section presents, in the order it presents them.
function rows(el) {
  return findAll(el, (node) => node.className?.split(/\s+/).includes('rdio-admin-row') && node.dataset.drag !== undefined);
}

function rowTitle(row) {
  const title = findAll(row, (node) => node.className?.split(/\s+/).includes('row-title'))[0];

  return title?.textContent ?? '';
}

function pageOrder(el) {
  return rows(el).map(rowTitle);
}

function workingOrder(name) {
  return sortedByOrder(getAdminContext().store.getState()[`section:${name}`].working).map((row) => row.label);
}

// The row's two move buttons, in the order the row presents them.
function moveButtons(row) {
  return findAll(row, (node) => node.tag === 'button' && /move (up|down)/i.test(node.getAttribute('aria-label') ?? ''));
}

function buttonNamed(row, direction) {
  return moveButtons(row).find((btn) => new RegExp(`^move ${direction} `, 'i').test(btn.getAttribute('aria-label') ?? '')) ?? null;
}

// The click a button answers: the event a mouse leaves and the keyboard's Enter and Space leave in
// exactly the same shape. The spy on preventDefault is what the reading reads: a control inside a
// row's summary line has to take the row's own activation away from the browser, or moving the row
// opens it.
function clickButton(button) {
  let prevented = false;

  button.dispatchEvent({
    type: 'click',
    target: button,
    bubbles: true,
    preventDefault: () => {
      prevented = true;
    },
  });

  return prevented;
}

describe('presentation/admin/row-builders — the keyboard moves a drag-sorted row', () => {
  it('carries a Move up and a Move down button on every drag-sorted row, naming the row it moves', () => {
    seed();
    const el = mount('rdio-admin-systems');

    const listed = rows(el);

    assert.equal(listed.length, 3, 'the section does not present the rows it sorts');

    const missing = listed.filter((row) => moveButtons(row).length !== 2).map(rowTitle);

    assert.deepEqual(missing, [], `rows without exactly two move buttons: ${missing.join(' | ')}`);

    const names = listed.map((row) => moveButtons(row).map((btn) => btn.getAttribute('aria-label')));

    assert.deepEqual(names, [
      ['Move up — County', 'Move down — County'],
      ['Move up — Metro', 'Move down — Metro'],
      ['Move up — Depot', 'Move down — Depot'],
    ]);
  });

  it('disables the button that would move the row past the ends of the list', () => {
    seed();
    const el = mount('rdio-admin-systems');

    const [first, middle, last] = rows(el);

    assert.equal(buttonNamed(first, 'up').disabled, true, 'the first row can move up past the top of the list');
    assert.equal(buttonNamed(first, 'down').disabled, false, 'the first row cannot move down');

    assert.equal(buttonNamed(middle, 'up').disabled, false, 'the middle row cannot move up');
    assert.equal(buttonNamed(middle, 'down').disabled, false, 'the middle row cannot move down');

    assert.equal(buttonNamed(last, 'up').disabled, false, 'the last row cannot move up');
    assert.equal(buttonNamed(last, 'down').disabled, true, 'the last row can move down past the bottom of the list');
  });

  it('moves the row down through the reorder the drag goes through, and the page shows it', () => {
    seed();
    const el = mount('rdio-admin-systems');

    const before = pageOrder(el);

    assert.deepEqual(before, ['County', 'Metro', 'Depot']);

    clickButton(buttonNamed(rows(el)[0], 'down'));

    const expected = reorderAfterDrag(getAdminContext().store.getState()['section:systems'].baseline, 0, 1);

    assert.deepEqual(workingOrder('systems'), ['Metro', 'County', 'Depot'], 'the store did not take the row where the button moved it');
    assert.deepEqual(pageOrder(el), ['Metro', 'County', 'Depot'], 'the page did not show the row where the button moved it');

    // The same persist path the drag uses: every row carries its place in the list as its order, so
    // the save bar sends the whole list's order, not the moved row's.
    assert.deepEqual(
      sortedByOrder(getAdminContext().store.getState()['section:systems'].working).map((row) => row.order),
      expected.map((row) => row.order),
      'the move left the list with orders the drag would not have written',
    );
  });

  it('moves the row up, and the ends follow where the rows end up', () => {
    seed();
    const el = mount('rdio-admin-systems');

    clickButton(buttonNamed(rows(el)[2], 'up'));

    assert.deepEqual(pageOrder(el), ['County', 'Depot', 'Metro'], 'the page did not show the row the button moved up');
    assert.deepEqual(workingOrder('systems'), ['County', 'Depot', 'Metro'], 'the store did not take the row where the button moved it');

    const [first, , last] = rows(el);

    assert.equal(buttonNamed(first, 'up').disabled, true, 'the row now at the top can move up past it');
    assert.equal(buttonNamed(last, 'down').disabled, true, 'the row now at the bottom can move down past it');
    assert.equal(buttonNamed(last, 'up').disabled, false, 'the row at the bottom cannot move up');
  });

  it('takes the row the buttons sit in away from opening when the keyboard moves it', () => {
    seed();
    const el = mount('rdio-admin-systems');

    const row = rows(el)[0];

    assert.equal(row.open, false, 'the reading starts on a closed row');

    clickButton(buttonNamed(row, 'down'));

    assert.equal(row.open, false, 'moving the row opened it');
  });

  it('carries the same buttons on the rows a section builds by hand', () => {
    seed();
    const el = mount('rdio-admin-groups');

    const listed = rows(el);

    assert.equal(listed.length, 3, 'the Groups section does not present the rows it sorts');

    const missing = listed.filter((row) => moveButtons(row).length !== 2).map(rowTitle);

    assert.deepEqual(missing, [], `rows without exactly two move buttons: ${missing.join(' | ')}`);

    assert.deepEqual(
      moveButtons(listed[0]).map((btn) => btn.getAttribute('aria-label')),
      ['Move up — Dispatch', 'Move down — Dispatch'],
    );

    assert.equal(buttonNamed(listed[0], 'up').disabled, true, 'the first Group row can move up past the top of the list');
    assert.equal(buttonNamed(listed[2], 'down').disabled, true, 'the last Group row can move down past the bottom of the list');

    clickButton(buttonNamed(rows(el)[1], 'down'));

    assert.deepEqual(pageOrder(el), ['Dispatch', 'Console', 'Interop'], 'the page did not show the row the button moved');
    assert.deepEqual(workingOrder('groups'), ['Dispatch', 'Console', 'Interop'], 'the store did not take the row where the button moved it');
  });

  it('names the buttons for the row as the row is named on the page', () => {
    seed();
    const el = mount('rdio-admin-groups');

    const row = rows(el)[0];
    const input = findAll(row, (node) => node.tag === 'input')[0];

    input.value = 'Dispatch North';
    input.dispatchEvent({ type: 'input', target: input });

    assert.equal(rowTitle(row), 'Dispatch North', 'the row did not take the name the operator typed');

    assert.deepEqual(
      moveButtons(row).map((btn) => btn.getAttribute('aria-label')),
      ['Move up — Dispatch North', 'Move down — Dispatch North'],
      'the buttons name a row the operator has renamed',
    );
  });
});
