// The dialog's focus trap, exercised through the events a keyboard produces.
//
// A dialog that lets the Tab key walk out of itself leaves the keyboard on the page behind a box
// that says it is modal: the operator keeps typing into controls they cannot see. The trap is the
// page's own keydown handler, and it has to answer the Tab key on the controls the dialog carries
// *at the moment the key is pressed* — a dialog whose content changes (a tree that re-renders, a
// control that appears, a control that is disabled) has to know its ends then, and it has to skip
// the controls a browser will not let the keyboard stop on: a hidden one and a disabled one.
//
// The readings are taken through the events the keyboard leaves: the keydown the Tab key produces,
// the focus the browser moves. The real key presses are walked in the browser harness; the shim has
// no keyboard of its own, so it carries the focus the browser moves.

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

// The two ends of the dialog's own route: the control the keyboard reaches first, and the one it
// reaches last. The trap is read against these, because these are the ends a Tab key pressed inside
// the dialog meets.
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

// The Tab key, pressed on the control the keyboard is standing on: the keydown the browser leaves on
// the control, bubbling to the dialog that traps it. The reading records whether the page took the
// key away from the browser — the preventDefault that stops the walk out of the dialog — and where
// the focus ended up.
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

  // The trap is a snapshot of the controls taken when the dialog opened. A dialog whose content
  // rebuilds — the selection tree re-renders on every checkbox the operator activates — has new
  // controls, and a snapshot taken at open time names ends that are no longer the ends.
  it('takes its ends from the controls the dialog carries when the key is pressed, not when it opened', () => {
    const body = document.createElement('div');

    body.append(control('First'), control('Second'));

    const dialog = open(body, []);

    // The content rebuilds: the tree the dialog carries is replaced by one that carries more
    // controls than the dialog was opened with.
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

    // A control the page has taken off the screen, and a control the page has disabled, sit at the
    // two ends of the dialog: the browser's own Tab route leaves both out, so the trap has to.
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
