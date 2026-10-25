// the action runs while its dialog and content are still on the page
// dismissal answers nothing was picked first, so a late pick is lost
// see the dialog-action-order maintainer note.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import '../../../common/frontend/presentation/ui/primitives.js';

import { openDialog } from '../../../common/frontend/presentation/ui/primitives.js';

// the dialog's action buttons, in the order the dialog bar presents them
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

// keydown dispatched on the focused control, bubbling to the dialog
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
    // actions with no handler, like the update prompt's No, still close the dialog
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
