// The dialog's actions, read at the moment they run.
//
// An action is how an operator answers a dialog: *Select* hands what the dialog's content holds to
// whatever opened it, *Cancel* hands nothing. Taking the dialog out of the page is the moment its
// content reports that nothing was picked — the systems and talkgroups tree, for instance, answers its
// own dismissal hook when it leaves the page, and a dismissal is the answer "nothing was picked". An
// action that runs *after* its own dialog has left the page therefore never gets to answer: the
// dismissal has already settled the exchange, and the operator's pick is thrown away.
//
// That is not a hypothetical. Measured in a real browser on the shipped administration application:
// unticking one talkgroup of a system the scope covered completely and pressing *Select* left the row
// reading exactly what it reads after *Cancel*, the section stayed clean, and the server kept the scope
// it had. The order the primitive's own click handler runs in is the cause — it closes the dialog and
// then calls the action — and these readings are what pin it, because nothing ever tested the wiring
// between this primitive and the content it carries.
//
// The readings are stated as facts about the page: the dialog and the content it carries are still on
// the page when the action runs, and both are off it afterwards. The shim cannot reach further: it does
// not run a disconnected callback, so the dismissal that a browser reports on the way out is a fact only
// a browser can read, and the outcome it produces is measured in the browser harness instead.
//
// What these readings do not touch is the dismissal path: Escape, the backdrop, and an action with no
// handler still take the dialog off the page.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import '../../../common/frontend/presentation/ui/primitives.js';

import { openDialog } from '../../../common/frontend/presentation/ui/primitives.js';

// The dialog's own action controls, in the order the dialog presents them: the bar the primitive builds
// holds them, and the content the operator answers through sits above it.
function actionButtons(dialog) {
  const bar = dialog.querySelector('.rdio-dialog-actions');

  return (bar?.children ?? []).filter((node) => node.tag === 'button');
}

function action(dialog, label) {
  const button = actionButtons(dialog).find((node) => node.textContent.trim() === label);

  assert.ok(button, `the dialog carries no ${label} action`);

  return button;
}

function control(label) {
  const el = document.createElement('button');

  el.type = 'button';
  el.textContent = label;

  return el;
}

// The content an action has to read: a control the operator activated, standing inside the dialog.
function content() {
  const box = document.createElement('div');

  box.append(control('Everything'), control('Metro'));

  return box;
}

// A key pressed on the control the keyboard is standing on: the keydown the browser leaves on the
// control, bubbling to the dialog that answers it.
function pressKey(el, key) {
  el.dispatchEvent({ type: 'keydown', key, target: el, preventDefault: () => {} });
}

describe('presentation/ui/primitives — the dialog answers with what its action hands back', () => {
  it('lets an action read its dialog and its content while they are still on the page', () => {
    const box = content();
    const seen = {};

    const dialog = openDialog({
      title: 'Systems and talkgroups',
      content: box,
      actions: [
        { label: 'Cancel' },
        {
          label: 'Select',
          onClick: () => {
            seen.dialog = dialog.isConnected;
            seen.content = box.isConnected;
          },
        },
      ],
    });

    assert.equal(dialog.isConnected, true, 'the opened dialog is not on the page');
    assert.equal(box.isConnected, true, 'the content the dialog was opened with is not on the page');

    action(dialog, 'Select').click();

    assert.equal(seen.dialog, true, 'the Select action ran after its own dialog had left the page');
    assert.equal(seen.content, true, 'the Select action ran after the content holding its answer had left the page');
  });

  it('takes the dialog and its content off the page once the action has answered', () => {
    const box = content();
    let answered = 0;

    const dialog = openDialog({
      title: 'Systems and talkgroups',
      content: box,
      actions: [
        { label: 'Cancel' },
        { label: 'Select', onClick: () => (answered += 1) },
      ],
    });

    action(dialog, 'Select').click();

    assert.equal(answered, 1, 'the Select action did not answer the dialog');
    assert.equal(dialog.isConnected, false, 'answering the dialog left it on the page');
    assert.equal(box.isConnected, false, 'the content stayed on the page after its dialog was answered');
  });

  it('still closes the dialog for an action that carries no handler', () => {
    // The webapps answer with a plain confirmation too: the update prompt's *No* button names nothing
    // but closing the box, and the operator who presses it is refused the reload.
    const dialog = openDialog({
      title: 'New version available',
      content: 'A new version of the scanner webapp is available. Would you like to reload the page?',
      actions: [{ label: 'No' }, { label: 'Yes' }],
    });

    assert.equal(dialog.isConnected, true, 'the confirmation is not on the page');

    action(dialog, 'No').click();

    assert.equal(dialog.isConnected, false, 'an action with no handler left the confirmation on the page');
  });

  it('still answers a dialog the operator dismissed with the keyboard', () => {
    const opener = control('Open the dialog');

    document.body.append(opener);
    opener.focus();

    const dialog = openDialog({
      title: 'Systems and talkgroups',
      content: content(),
      actions: [{ label: 'Cancel' }, { label: 'Select' }],
    });

    pressKey(action(dialog, 'Cancel'), 'Escape');

    assert.equal(dialog.isConnected, false, 'Escape left the dialog on the page');
    assert.equal(document.activeElement, opener, 'dismissing the dialog did not put the keyboard back where it started');
  });
});
