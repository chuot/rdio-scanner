// The listener's panels, and the keyboard that has to travel with them.
//
// The search and select panels are drawers the listener opens over its screen. The pointer's route is
// obvious: the panel slides out, the operator works in it, the operator clicks the arrow that shuts
// it. The keyboard's route has to be the same journey, and a panel that opens without taking the
// keyboard with it leaves the operator on the button they pressed, with the panel they are looking at
// sitting behind them in the tab order — every Tab key walks through the page to reach it. A panel
// that closes without giving the keyboard back leaves the focus on a control that has just left the
// page, and the browser puts the operator at the top of the document.
//
// The readings are taken on the control the operator pressed, through the click event a mouse leaves
// and the keyboard's Enter and Space leave in the same shape.

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

// The shell, rendered the way the page renders it. The promo the shell schedules is a ten second
// timer the browser owns; the shim does not model the clock, so the timer is handed to the test
// during the render and left unrun, and the real clock is put back before the reading is taken.
function shell() {
  document.body.replaceChildren();

  // Opening the search panel asks the listener to search the recorded calls: the shim has no network,
  // and the reading is about where the keyboard ends up, not about what the search answers.
  getAppContext().useCases.searchCalls = () => {};

  const el = document.createElement('rdio-app');

  const realSetTimeout = globalThis.setTimeout;

  globalThis.setTimeout = () => 0;

  try {
    el.connectedCallback();
  } finally {
    globalThis.setTimeout = realSetTimeout;
  }

  document.body.append(el);

  return el;
}

function control(el, selector) {
  return findAll(el, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes(selector))[0]
    ?? findAll(el, (node) => node.tag === 'button' && node.dataset.action === selector)[0]
    ?? null;
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
});
