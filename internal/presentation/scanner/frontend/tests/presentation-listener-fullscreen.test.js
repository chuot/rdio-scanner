// the listener's fullscreen and its keyboard route: the display fills
// the monitor on a double-click, a gesture with no keyboard equivalent,
// so the state needs a control the keyboard can stop on.
// see the fullscreen-keyboard-control maintainer note.

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

// the platform's fullscreen, as the page asks for it: the way in is
// the element's own request, the way out the document's, because the
// element never carries it. the shim records both the page's calls.
// see the fullscreen-exit-api maintainer note.
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

  // the promo is a ten second timer the browser owns and the display
  // keeps its clock and progress on intervals of its own: the shim
  // hands those timers to the test during the render, left unrun.
  // the real clock comes back before the reading is taken.
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
