// The listener's fullscreen, and the keyboard's route into it.
//
// The listener's screen goes fullscreen on a double-click: the display fills the monitor and the
// operator watches the call list. A double-click is a pointer gesture — there is no keyboard
// equivalent of it, so an operator at the keyboard has no way into the fullscreen screen at all, and
// an operator who cannot hold a pointer steady for two presses in the same place has no way either.
// The state needs a control the keyboard can stop on, that says which way it will move the page, and
// that says where the page is.
//
// The double-click route stays: it is the gesture the listener's operators use today, and a control
// beside it takes the same state the same page keeps.
//
// The shim carries no fullscreen of its own: the test hands the page the platform's two calls and the
// state the browser reports, and the reading is taken on the control the operator activates.

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

// The platform's fullscreen, as the page asks for it. The browser answers the page's request with
// the state it keeps, and tells the page about it with the event the page listens for; the test
// carries both, and records the calls the page makes.
//
// The way in is the element's own request. The way out is the document's: Chrome and Safari carry
// it on the document, and only Firefox puts it on the element. A page that asks the element to
// leave is asking a question the browser never put on the element, and the page is left with no
// way out of the state it entered.
function fullscreenPlatform() {
  const calls = [];

  const root = {
    requestFullscreen() {
      calls.push('request');
      document.fullscreenElement = root;
      document.dispatchEvent({ type: 'fullscreenchange', target: root });
    },
  };

  document.documentElement = root;
  document.fullscreenElement = null;

  document.exitFullscreen = () => {
    calls.push('exit');
    document.fullscreenElement = null;
    document.dispatchEvent({ type: 'fullscreenchange', target: root });
  };

  return calls;
}

function shell() {
  document.body.replaceChildren();

  getAppContext().useCases.searchCalls = () => {};

  const el = document.createElement('rdio-app');

  // The promo the shell schedules is a ten second timer the browser owns, and the display keeps the
  // clock and the progress bar moving on intervals of its own: the shim does not model the clock, so
  // the timers are handed to the test during the render and left unrun, and the real clock is put
  // back before the reading is taken.
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

// The control the keyboard stops on: the one the page names for the fullscreen state.
function fullscreenControl(el) {
  return findAll(el, (node) => node.tag === 'button'
    && /fullscreen/i.test(node.getAttribute('aria-label') ?? node.textContent))[0] ?? null;
}

function activate(el) {
  el.dispatchEvent({ type: 'click', target: el, bubbles: true });
}

describe('listener fullscreen — a control the keyboard can operate', () => {
  it('carries a control that names the fullscreen state it will move the page into', () => {
    fullscreenPlatform();
    const el = shell();
    const control = fullscreenControl(el);

    assert.ok(control, 'the listener carries no control naming fullscreen');
    assert.match(control.getAttribute('aria-label') ?? '', /fullscreen/i,
      'the control the keyboard stops on does not name what it does');
    assert.ok(control.getAttribute('aria-pressed') !== null,
      'the control does not say which way the page is');
  });

  it('moves the page into fullscreen from the keyboard', () => {
    const calls = fullscreenPlatform();
    const el = shell();
    const control = fullscreenControl(el);

    control.focus();
    activate(control);

    assert.deepEqual(calls, ['request'], 'the control did not ask the page into fullscreen');
    assert.equal(control.getAttribute('aria-pressed'), 'true', 'the control does not say the page is in fullscreen');
  });

  it('moves the page out of fullscreen from the keyboard', () => {
    const calls = fullscreenPlatform();
    const el = shell();
    const control = fullscreenControl(el);

    activate(control);
    activate(control);

    assert.deepEqual(calls, ['request', 'exit'], 'the control did not ask the page out of fullscreen');
    assert.equal(control.getAttribute('aria-pressed'), 'false', 'the control does not say the page is back on the window');
  });

  it('names the way it will move the page, the way the page is', () => {
    fullscreenPlatform();
    const el = shell();
    const control = fullscreenControl(el);

    assert.match(control.getAttribute('aria-label'), /fullscreen/i);

    activate(control);

    const label = control.getAttribute('aria-label');

    assert.match(label, /exit|leave|full/i, `the control says nothing about the state it is in: ${label}`);
    assert.match(label, /exit/i, 'the control does not name the move it will make: the page is already fullscreen');

    activate(control);

    assert.equal(control.getAttribute('aria-label'), 'Fullscreen', 'the control did not name the move back into fullscreen');
  });

  it('follows the fullscreen the page reaches by the double-click route', () => {
    const calls = fullscreenPlatform();
    const el = shell();
    const control = fullscreenControl(el);
    const display = findAll(el, (node) => node.tag === 'rdio-display')[0];

    display.dispatchEvent({ type: 'dblclick', target: display, bubbles: true });

    assert.deepEqual(calls, ['request'], 'the double-click route no longer asks the page into fullscreen');
    assert.equal(control.getAttribute('aria-pressed'), 'true',
      'the control does not say the page is in fullscreen after the double-click got it there');

    activate(control);

    assert.deepEqual(calls, ['request', 'exit'], 'the control did not bring the page out of the fullscreen the pointer put it in');
  });

  it('keeps the control out of the fullscreen request the page cannot answer', () => {
    // A browser that offers no fullscreen at all: the control is a control the operator can stop on,
    // and it says nothing the page cannot do.
    fullscreenPlatform();
    delete document.documentElement.requestFullscreen;

    const el = shell();
    const control = fullscreenControl(el);

    control.focus();

    assert.doesNotThrow(() => activate(control), 'the control broke on a page that cannot go fullscreen');
    assert.equal(control.getAttribute('aria-pressed'), 'false', 'the control claimed a state the page cannot reach');
  });
});
