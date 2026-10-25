// the dialog's tab focus trap, read through the keyboard events it answers
// it reads its ends at keypress time and skips hidden and disabled controls
// see the dialog-focus-trap maintainer note.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import '../../../common/frontend/presentation/ui/primitives.js';

import { openDialog } from '../../../common/frontend/presentation/ui/primitives.js';

import { findAll } from './dom-shim.js';

// The controls the keyboard stops on, in the order the page presents them.
function focusables(root) {
  return findAll(root, (node) => ['button', 'input', 'select', 'textarea'].includes(node.tag));
}

// the first and last focusable controls inside the dialog
function ends(dialog) {
  const all = focusables(dialog);

  return [all[0], all.at(-1)];
}

function control(label) {
  const el = document.createElement('button');
  el.type = 'button';
  el.textContent = label;

  return el;
}

// The dialog, built the way a section builds it: content, then the actions the operator answers with.
function open(content, actions = [{ label: 'Cancel' }, { label: 'Select' }]) {
  const dialog = openDialog({ title: 'Systems and talkgroups', content, actions });

  return dialog;
}

// Tab posted on the focused control, bubbling to the dialog
// records whether preventDefault stopped the walk, and where focus landed
function pressTab(el, { shift = false } = {}) {
  let prevented = false;

  el.dispatchEvent({
    type: 'keydown',
    key: 'Tab',
    shiftKey: shift,
    target: el,
    preventDefault: () => {
      prevented = true;
    },
  });

  return prevented;
}

describe('presentation/ui/primitives — the dialog trap, at the moment the key is pressed', () => {
  it('opens with the keyboard inside the dialog', () => {
    const body = document.createElement('div');
    const first = control('First');

    body.append(first, control('Last'));

    const dialog = open(body);

    assert.equal(document.activeElement, focusables(dialog)[0], 'the keyboard did not start inside the dialog');

    dialog.close();
  });

  it('wraps the Tab key at the ends of the dialog', () => {
    const body = document.createElement('div');

    body.append(control('First'), control('Last'));

    const dialog = open(body);
    const [first, last] = ends(dialog);

    last.focus();

    assert.equal(pressTab(last), true, 'the Tab key on the last control was left to the browser');
    assert.equal(document.activeElement, first, 'the Tab key on the last control did not wrap to the first');

    assert.equal(pressTab(first, { shift: true }), true, 'the Shift+Tab key on the first control was left to the browser');
    assert.equal(document.activeElement, last, 'the Shift+Tab key on the first control did not wrap to the last');

    dialog.close();
  });

  // the trap's ends are the dialog's live controls, not an open-time snapshot
  it('takes its ends from the controls the dialog carries when the key is pressed, not when it opened', () => {
    const body = document.createElement('div');

    body.append(control('First'), control('Second'));

    const dialog = open(body, []);

    // the content rebuilds, carrying one more control than at open
    const added = control('Third');

    body.append(added);

    const [first, last] = ends(dialog);

    assert.equal(focusables(dialog).length, 3, 'the dialog does not carry the control its content rebuilt with');

    last.focus();

    assert.equal(pressTab(last), true, 'the Tab key on the control the rebuild added was left to the browser');
    assert.equal(document.activeElement, first, 'the Tab key on the rebuilt end did not wrap to the first');

    dialog.close();
  });

  it('skips the controls a browser will not let the keyboard stop on', () => {
    const body = document.createElement('div');

    // hidden and disabled controls at both ends: the browser tab route skips them
    const hidden = control('Hidden first');

    hidden.hidden = true;

    const disabled = control('Disabled last');

    disabled.disabled = true;

    body.append(hidden, control('First reachable'), control('Last reachable'), disabled);

    const dialog = open(body, []);

    const reachable = [body.children[1], body.children[2]];

    assert.notEqual(document.activeElement, hidden, 'the keyboard was placed on a control the page has hidden');

    // The keyboard stands on the last control the browser will stop on and presses Tab.
    reachable[1].focus();

    assert.equal(pressTab(reachable[1]), true, 'the Tab key on the last reachable control was left to the browser');
    assert.equal(document.activeElement, reachable[0], 'the Tab key did not wrap to the first reachable control');

    assert.equal(pressTab(reachable[0], { shift: true }), true, 'the Shift+Tab key on the first reachable control was left to the browser');
    assert.equal(document.activeElement, reachable[1], 'the Shift+Tab key did not wrap to the last reachable control');

    dialog.close();
  });

  it('returns the keyboard to the control that opened the dialog', () => {
    const opener = control('Open the dialog');

    document.body.append(opener);

    opener.focus();

    const body = document.createElement('div');

    body.append(control('First'));

    const dialog = open(body);

    assert.equal(document.activeElement, focusables(dialog)[0], 'the keyboard did not start inside the dialog');

    dialog.close();

    assert.equal(document.activeElement, opener, 'closing the dialog did not put the keyboard back where it started');
  });
});
