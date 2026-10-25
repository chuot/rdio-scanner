// the notification the listener puts on the screen: the message, the
// surface it sits on, and the way it leaves. the box plays a 150ms enter
// and a 75ms exit, and the animations go away when the operator asks
// for less motion, so the hide cannot depend on them to leave the page.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { afterEach, describe, it } from 'node:test';

import { dismissSnackbar, showSnackbar } from '../../../common/frontend/presentation/ui/primitives.js';

// The platform's motion preference, as the component asks for it. The shim carries no media
// queries: this is the one the component reads.
function motionPreference(reduced) {
  globalThis.matchMedia = (query) => ({
    media: query,
    matches: /prefers-reduced-motion/.test(query) ? reduced : false,
  });
}

// the host the page carries: the listener's own carries the `material`
// attribute, which asks the primitive for the baseline's surface and
// animations; the administration application's does not.
function host({ material = true } = {}) {
  document.body.replaceChildren();

  const el = document.createElement('rdio-snackbar');

  if (material) el.setAttribute('material', '');

  document.body.appendChild(el);
  el.connectedCallback();

  return el;
}

const exit = (el) => el.className.split(/\s+/).includes('rdio-snackbar-exit');

afterEach(() => {
  delete globalThis.matchMedia;
});

describe('presentation/ui/rdio-snackbar — how the notification leaves', () => {
  it('hides the notification under a reduced motion preference, with no animation to wait for', () => {
    motionPreference(true);

    const el = host();

    showSnackbar('Dispatch failed', null);
    dismissSnackbar();

    assert.equal(el.hidden, true, 'the notification is still on the page with motion suppressed');
    assert.deepEqual(el.children, [], 'the notification left its surface behind');
    assert.equal(el.textContent, '');
  });

  it('hides the notification when the exit animation ends, when motion is not reduced', () => {
    motionPreference(false);

    const el = host();

    showSnackbar('Dispatch failed', null);
    dismissSnackbar();

    assert.ok(exit(el), 'the exit was not asked for');
    assert.notEqual(el.hidden, true, 'the box left before its exit ran');

    el.dispatchEvent({ type: 'animationend', animationName: 'rdio-snackbar-exit' });

    assert.equal(el.hidden, true);
    assert.deepEqual(el.children, []);
  });

  it('ignores the end of an animation that is not the exit', () => {
    motionPreference(false);

    const el = host();

    showSnackbar('Dispatch failed', null);

    el.dispatchEvent({ type: 'animationend', animationName: 'rdio-snackbar-enter' });

    assert.notEqual(el.hidden, true);
    assert.equal(el.textContent, 'Dispatch failed');
  });

  it('shows a notification that arrives while the exit is running', () => {
    motionPreference(false);

    const el = host();

    showSnackbar('Dispatch failed', null);
    dismissSnackbar();
    showSnackbar('Dispatch started', null);

    assert.notEqual(el.hidden, true);
    assert.equal(el.textContent, 'Dispatch started');
    assert.ok(!exit(el), 'the box is still on its way out while showing a new message');
  });

  it('hides the administration notification without an animation, as it always did', () => {
    motionPreference(false);

    const el = host({ material: false });

    showSnackbar('Saved', null);
    dismissSnackbar();

    assert.equal(el.hidden, true);
  });
});
