// the listener's panels and the keyboard that travels with them: a
// panel that opens without taking the keyboard leaves it behind the
// panel in the tab order, and a closing one can lose it to the top.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { getAppContext } from '../application/context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/listener/rdio-app.js';
import '../presentation/listener/rdio-controls.js';
import '../presentation/listener/rdio-display.js';
import '../presentation/listener/rdio-search-panel.js';
import '../presentation/listener/rdio-select-panel.js';
import '../presentation/listener/rdio-status.js';

import { findAll } from './dom-shim.js';

// the shell, rendered the way the page renders it: the shim connects
// every element the shell's markup carries, as a browser does. the
// shim does not model the clock, so the promo and display timers are
// handed to the test and left unrun until the reading is taken.
function shell() {
  document.body.replaceChildren();

  // the control bar answers its buttons only when the listener has a
  // configuration to work with: a listener with no systems has no call
  // to search. the config the reading needs has talkgroups to select,
  // calls to search, and no contact address, so help stays off the page.
  getAppContext().store.setState({
    ...getAppContext().store.getState(),
    config: {
      email: '',
      groups: { Dispatch: {}, Interop: {} },
      tags: { Fire: {} },
      systems: [{
        id: 1,
        label: 'Metro',
        talkgroups: [
          { id: 10, label: 'Fire dispatch', groups: ['Dispatch'], tag: 'Fire' },
          { id: 11, label: 'Tac dispatch', groups: ['Interop'], tag: 'Fire' },
        ],
      }],
    },
  });

  // Opening the search panel asks the listener to search the recorded calls: the shim has no network,
  // and the reading is about where the keyboard ends up, not about what the search answers.
  getAppContext().useCases.searchCalls = () => {};

  const el = document.createElement('rdio-app');

  const realSetTimeout = globalThis.setTimeout;
  const realSetInterval = globalThis.setInterval;

  globalThis.setTimeout = () => 0;
  globalThis.setInterval = () => 0;

  try {
    el.connectedCallback();
  } finally {
    globalThis.setTimeout = realSetTimeout;
    globalThis.setInterval = realSetInterval;
  }

  document.body.append(el);

  return el;
}

// The control the listener's own control bar uses to open a panel.
function opener(el, name) {
  return findAll(el, (node) => node.tag === 'button' && node.dataset.action === name)[0];
}

function panel(el, name) {
  return findAll(el, (node) => node.tag === 'aside' && node.className?.split(/\s+/).includes('panel')
    && node.className?.split(/\s+/).includes(name))[0];
}

function closeControl(panel) {
  return findAll(panel, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes('panel-close'))[0];
}

function focusables(root) {
  return findAll(root, (node) => ['button', 'input', 'select', 'textarea'].includes(node.tag)
    || node.attributes.role === 'switch' || node.attributes.tabindex !== undefined);
}

function inside(root, node) {
  for (let el = node; el; el = el.parent) {
    if (el === root) return true;
  }

  return false;
}

// The keyboard's Enter on a control: the click event the browser leaves on it.
function activate(el) {
  el.dispatchEvent({ type: 'click', target: el, bubbles: true });
}

// A search that answered: the panel paints its rows from the answer, and rebuilds the whole table on
// the next one. The rows are the calls the history holds, in the shape the answer carries them.
const CALLS = [
  {
    id: 101,
    system: 1,
    talkgroup: 10,
    dateTime: new Date(2026, 9, 6, 9, 15).toISOString(),
    audioLength: 12,
    systemData: { label: 'Metro' },
    talkgroupData: { label: 'Fire dispatch', name: 'Fire fire ground' },
  },
  {
    id: 102,
    system: 1,
    talkgroup: 11,
    dateTime: new Date(2026, 9, 6, 10, 30).toISOString(),
    audioLength: 30,
    systemData: { label: 'Metro' },
    talkgroupData: { label: 'Tac dispatch', name: 'Metro tac' },
  },
];

function answered(rows) {
  const state = getAppContext().store.getState();

  getAppContext().store.setState({
    playbackList: { count: rows.length, options: { limit: 200, offset: 0 }, results: rows },
    playbackListSeq: state.playbackListSeq + 1,
  });
}

describe('listener panels — the keyboard travels with the panel', () => {
  it('takes the keyboard into the search panel when the panel opens', () => {
    const el = shell();
    const button = opener(el, 'search');

    button.focus();
    activate(button);

    const opened = panel(el, 'search');

    assert.ok(opened, 'the shell did not open the search panel');
    assert.equal(opened.hidden, false, 'the panel is still hidden');
    assert.ok(inside(opened, document.activeElement),
      `the keyboard stayed outside the panel it opened: ${document.activeElement?.tag}`);
    assert.ok(focusables(opened).includes(document.activeElement),
      'the keyboard was placed on something the keyboard cannot stop on');
  });

  it('takes the keyboard into the select panel when the panel opens', () => {
    const el = shell();
    const button = opener(el, 'select');

    button.focus();
    activate(button);

    const opened = panel(el, 'select');

    assert.ok(opened, 'the shell did not open the select panel');
    assert.ok(inside(opened, document.activeElement),
      `the keyboard stayed outside the panel it opened: ${document.activeElement?.tag}`);
  });

  it('gives the keyboard back to the control that opened the panel when the panel closes', () => {
    const el = shell();
    const button = opener(el, 'search');

    button.focus();
    activate(button);

    closeControl(panel(el, 'search')).click();

    assert.equal(document.activeElement, button,
      `closing the panel left the keyboard on ${document.activeElement?.tag}, not on the control that opened it`);
  });

  it('gives the keyboard back when the panel is closed by the keyboard, not by the pointer', () => {
    const el = shell();
    const button = opener(el, 'select');

    button.focus();
    activate(button);

    const close = closeControl(panel(el, 'select'));

    close.focus();
    activate(close);

    assert.equal(document.activeElement, button, 'the keyboard was not returned to the control that opened the panel');
  });

  it('gives the keyboard back to the control the panel was opened from, not to the page', () => {
    const el = shell();
    const search = opener(el, 'search');
    const select = opener(el, 'select');

    search.focus();
    activate(search);
    closeControl(panel(el, 'search')).click();

    assert.equal(document.activeElement, search, 'the first panel did not return its opener');

    select.focus();
    activate(select);
    closeControl(panel(el, 'select')).click();

    assert.equal(document.activeElement, select, 'the second panel returned the opener of the other panel');
  });

  it('opens the panel with the keyboard on the control the operator is standing on', () => {
    const el = shell();
    const button = opener(el, 'search');

    // The keyboard's Enter: the control answers with the click the browser leaves on it, and the
    // panel opens with the keyboard inside it.
    button.focus();
    activate(button);

    const opened = panel(el, 'search');

    assert.ok(inside(opened, document.activeElement), 'the keyboard did not travel with the panel it opened by keyboard');

    closeControl(opened).focus();
    activate(closeControl(opened));

    assert.equal(document.activeElement, button, 'the keyboard did not come back to the control it opened the panel from');
  });

  it('keeps the keyboard inside the search panel when the panels own answer repaints the list', () => {
    const el = shell();
    const button = opener(el, 'search');

    // The panel answers once, so its table holds rows, and the place the keyboard is taken to when
    // the panel opens is chosen from the controls the panel is showing.
    button.focus();
    activate(button);

    const opened = panel(el, 'search');

    answered(CALLS);

    closeControl(opened).click();

    button.focus();
    activate(button);

    // Opening the panel asks for a search, and the answer rebuilds every row of the table.
    answered(CALLS.slice(1));

    assert.ok(inside(opened, document.activeElement),
      `the panel's own answer took the keyboard out of the panel it opened: ${document.activeElement?.tag}`);
  });
});
