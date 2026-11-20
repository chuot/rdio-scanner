// What the control beside an api key does when the page has no clipboard to write into. A plain HTTP address
// handed to another machine is not a secure context, and the browser gives such a page no navigator.clipboard
// at all: the key the row shows has to reach the operator some other way, and a press that copied nothing has
// to say so instead of staying quiet. The page is read with the row open, the way the operator reaches the
// control, and with or without the asynchronous clipboard, which is what the two origins it answers on look
// like from inside the page.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-apikeys.js';

import {
  clearCopyCommandCalls,
  clearHandlerFailures,
  connectFields,
  copyCommandCalls,
  findAll,
  handlerFailures,
  setCopyCommandAnswer,
  texts,
} from './dom-shim.js';

// The key the row shows, in the shape a generated key takes: 36 characters, in the groups a version-4 uuid
// writes them into. The characters are a stand-in, not a key anyone's deployment holds.
const KEY = 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee';

const SUCCESS_LINE = 'Key copied to clipboard';
const FAILURE_LINE = 'Could not copy the key. It is selected — press Ctrl+C to copy it.';

const ROWS = [
  { id: 20, order: 1, disabled: false, key: KEY, ident: 'Recorder', systems: '*' },
  { id: 21, order: 2, disabled: false, key: 'key-two', ident: 'Booth line', systems: '*' },
];

function seed() {
  const { store } = getAdminContext();

  const baseline = ROWS.map((row) => ({ ...row }));

  // a snackbar, a live region, a copy command, or a page failure another reading left is not this reading's.
  document.body.replaceChildren();
  clearCopyCommandCalls();
  clearHandlerFailures();
  setCopyCommandAnswer(true);

  store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    alerts: {},
    'section:apikeys': {
      baseline,
      working: baseline.map((row) => ({ ...row })),
      dirty: false,
      invalid: false,
      conflict: false,
    },
  });

  return store;
}

function attach() {
  const el = new (customElements.get('rdio-admin-apikeys'))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

function rows(el) {
  return findAll(el, (node) => node.tag === 'details' && node.dataset.key !== undefined);
}

function openRows(el) {
  return rows(el).filter((row) => row.open);
}

// the first row opened: the copy control lives in the opened row, which is where the operator reaches it.
function openFirstRow(el) {
  const first = rows(el)[0];

  first.open = !first.open;
  first.dispatchEvent({ type: 'toggle', target: first, bubbles: false });

  const [open] = openRows(el);

  assert.ok(open, 'the api key list did not open the row this reading activated');

  return open;
}

function field(row, title) {
  return findAll(row, (node) => node.tag === 'rdio-form-field').find((node) => texts(findAll(node, (child) => child.tag === 'label'))[0] === title);
}

// the opened row's own visible key field, and the control its value is typed into.
function keyInput(row) {
  const input = findAll(field(row, 'Key'), (node) => node.tag === 'input')[0];

  assert.ok(input, 'the opened api key row carries no key field to read the copy from');

  return input;
}

function copyControl(row) {
  return findAll(row, (node) => node.tag === 'button' && node.getAttribute('aria-label') === 'Copy key to clipboard')[0] ?? null;
}

function snackbarSaid() {
  return document.querySelector('rdio-snackbar')?.textContent ?? '';
}

function regionSaid() {
  return document.querySelector('rdio-live-region')?.textContent.trim() ?? '';
}

// what the press left in the page's console: a browser reports an event handler that failed and carries on
// past it, and a reading that expects the press to succeed has to hear that report.
function pageFailures() {
  return handlerFailures().map((reason) => `${reason?.name ?? 'Error'}: ${reason?.message ?? String(reason)}`);
}

function settle() {
  return new Promise((resolve) => setImmediate(resolve));
}

function pressCopy(row) {
  const control = copyControl(row);

  assert.ok(control, 'the opened api key row carries no control that copies its key');

  control.click();
}

async function assertNothingFailedInThePage(context) {
  await settle();

  assert.deepEqual(pageFailures(), [], `${context}: the press left ${pageFailures().join(', ')} in the page's console`);
}

// the copy counts as a copy: the snackbar says the key was copied, and nothing in the page failed.
function assertCopied(context) {
  assert.equal(snackbarSaid(), SUCCESS_LINE, `${context} said ${JSON.stringify(snackbarSaid())}`);
}

// the copy did not happen, and the control says so where the operator reads it and where a screen reader hears
// it: no success line, the failure line in the snackbar and in the page's polite live region.
function assertSaidNotCopied(context) {
  assert.notEqual(snackbarSaid(), SUCCESS_LINE, `${context} reported a copied key it did not copy`);
  assert.equal(snackbarSaid(), FAILURE_LINE, `${context} showed ${JSON.stringify(snackbarSaid())} in the snackbar, not the failure the operator needs to read`);
  assert.equal(regionSaid(), FAILURE_LINE, `${context} left the polite live region saying ${JSON.stringify(regionSaid())}, so a screen reader was never told the key was not copied`);
}

// the whole key is in the selection, and the keyboard stands on the field holding it: the state the operator's
// own Ctrl+C finishes the copy from.
function assertKeySelected(input, context) {
  assert.equal(document.activeElement, input, `${context}: the keyboard was left on ${JSON.stringify(document.activeElement?.tag)}, not on the key field the control copied from`);
  assert.equal(input.selectionStart, 0, `${context}: the selection starts at ${input.selectionStart}, not at the start of the key`);
  assert.equal(input.selectionEnd, KEY.length, `${context}: the selection ends at ${input.selectionEnd}, not past the whole ${KEY.length}-character key`);
}

// the clipboard a secure origin gives the page. The shim installs none of its own, which is what lets a reading
// run with no clipboard at all: a reading that needs one, or one the browser turns down, owns it.
function installClipboard({ refuses = false } = {}) {
  const writes = [];
  const previous = globalThis.navigator.clipboard;

  globalThis.navigator.clipboard = {
    writeText: async (value) => {
      writes.push(value);

      if (refuses) throw new Error('the browser refused the write');
    },
  };

  const restore = () => {
    if (previous === undefined) delete globalThis.navigator.clipboard;
    else globalThis.navigator.clipboard = previous;
  };

  return { writes, restore };
}

// On the origin the browser gives no clipboard, the press still copies: it asks the document to copy the opened
// row's own visible key field, with the whole key in the selection.
test('pressing the copy control on a page with no clipboard copies the key through the document copy command', async () => {
  assert.equal(globalThis.navigator.clipboard, undefined, 'the shim installed a clipboard this reading is meant to run without');

  seed();

  const el = attach();

  const row = openFirstRow(el);
  const input = keyInput(row);

  pressCopy(row);

  await assertNothingFailedInThePage('the press on a page with no clipboard');

  const calls = copyCommandCalls();

  assert.equal(calls.length, 1, `the press issued ${calls.length} copy command(s) on a page the browser gave no clipboard: the key the row shows has nowhere else to go`);
  assert.equal(calls[0].command, 'copy', `the document was asked to run ${JSON.stringify(calls[0].command)}, not a copy`);
  assert.equal(calls[0].control, input, 'the copy command ran over something other than the opened row\'s own visible key field');
  assert.equal(calls[0].selectedText, KEY, `the copy command ran over ${JSON.stringify(calls[0].selectedText)}, not the whole key the row shows`);

  assertCopied('the press that copied the key through the document copy command');
});

// The copy is the only change the press makes: the key is selected for the operator's own Ctrl+C, the row stays
// open, and the key stays where the section had it.
test('pressing the copy control on a page with no clipboard leaves the key selected and commits nothing', async () => {
  const store = seed();

  const el = attach();

  const row = openFirstRow(el);
  const input = keyInput(row);

  pressCopy(row);

  await assertNothingFailedInThePage('the press on a page with no clipboard');

  assertKeySelected(input, 'the press that copied the key through the document copy command');

  assert.equal(openRows(el).length, 1, 'copying a key changed which api key row is open');
  assert.equal(rows(el).length, ROWS.length, 'copying a key added or removed an api key row');
  assert.equal(store.getState()['section:apikeys'].working[0].key, KEY, 'copying a key wrote the key into the section being edited');
  assert.equal(store.getState()['section:apikeys'].dirty, false, 'copying a key made the section look unsaved');
});

// A document that runs no copy is a press that copied nothing, and the operator is told.
test('a document copy command that runs no copy is said as a failure, with the key left selected', async () => {
  seed();

  setCopyCommandAnswer(false);

  const el = attach();

  const row = openFirstRow(el);
  const input = keyInput(row);

  pressCopy(row);

  await assertNothingFailedInThePage('the press whose copy command ran no copy');

  const calls = copyCommandCalls();

  assert.equal(calls.length, 1, `the press issued ${calls.length} copy command(s) on a page the browser gave no clipboard`);
  assert.equal(calls[0].selectedText, KEY, `the copy command ran over ${JSON.stringify(calls[0].selectedText)}, not the whole key the row shows`);

  assertSaidNotCopied('the press whose copy command ran no copy');

  assertKeySelected(input, 'the press whose copy command ran no copy');
});

// A copy command answers true even when the selection it ran over covered none of the value it was asked
// over, and nothing reaches the clipboard. That answer is not a copied key.
test('a document copy command that answers true over a selection covering none of the key is not reported as a copy', async () => {
  seed();

  const el = attach();

  const row = openFirstRow(el);
  const input = keyInput(row);

  input.focus();

  // the field's selection stays where its caret was, which is the state a copy command answers true over
  // while copying nothing: what the browser reading shows as a paste recovering only the value it seeded.
  input.select = () => {};

  pressCopy(row);

  await assertNothingFailedInThePage('the press whose copy command copied nothing');

  const calls = copyCommandCalls();

  assert.equal(calls.length, 1, `the press issued ${calls.length} copy command(s) on a page the browser gave no clipboard`);
  assert.equal(calls[0].selectedText, '', `the copy command ran over ${JSON.stringify(calls[0].selectedText)}, which is what this reading is about`);

  assertSaidNotCopied('the press whose copy command ran over a selection covering none of the key');
});

// A field holding nothing is not a copied key, whatever the command the page ran answers.
test('a key field holding nothing is not reported as copied', async () => {
  seed();

  const el = attach();

  const row = openFirstRow(el);
  const input = keyInput(row);

  input.value = '';

  pressCopy(row);

  await assertNothingFailedInThePage('the press over an empty key field');

  assertSaidNotCopied('the press over an empty key field');
});

// On an origin that gives the page the asynchronous clipboard, that call is what copies the key, and the
// selection command is not used there.
test('the copy control writes the key through the clipboard where the page has one', async () => {
  const { writes, restore } = installClipboard();

  try {
    seed();

    const el = attach();

    const row = openFirstRow(el);

    pressCopy(row);

    await assertNothingFailedInThePage('the press on a page with a clipboard');

    assert.deepEqual(writes, [KEY], `the clipboard was handed ${JSON.stringify(writes)}, not the key the row shows`);
    assert.equal(copyCommandCalls().length, 0, 'the press ran the document copy command on a page the browser gave an asynchronous clipboard');

    assertCopied('the press that wrote the key into the clipboard');
  } finally {
    restore();
  }
});

// A clipboard write the browser turns down is caught by the control, not left as a page failure: the press says
// it did not copy, and leaves the key where the operator can take it themselves.
test('a clipboard write the browser turns down is said as a failure, with the key left selected', async () => {
  const { writes, restore } = installClipboard({ refuses: true });

  try {
    seed();

    const el = attach();

    const row = openFirstRow(el);
    const input = keyInput(row);

    pressCopy(row);

    await assertNothingFailedInThePage('the press whose clipboard write the browser turned down');

    assert.equal(writes.length, 1, `the control asked the clipboard for the key ${writes.length} time(s): the press never reached the write the browser turns down`);
    assert.equal(copyCommandCalls().length, 0, `the press ran ${copyCommandCalls().length} document copy command(s) on a page the browser gave an asynchronous clipboard`);

    assertSaidNotCopied('the press whose clipboard write the browser turned down');

    assertKeySelected(input, 'the press whose clipboard write the browser turned down');
  } finally {
    restore();
  }
});

// Writing nothing over a clipboard that holds something would take the copy the operator already had: an empty
// key field is not written, and is not reported as copied.
test('a key field holding nothing is not written into the clipboard a page has', async () => {
  const { writes, restore } = installClipboard();

  try {
    seed();

    const el = attach();

    const row = openFirstRow(el);
    const input = keyInput(row);

    input.value = '';

    pressCopy(row);

    await assertNothingFailedInThePage('the press over an empty key field on a page with a clipboard');

    assert.deepEqual(writes, [], `the clipboard was handed ${JSON.stringify(writes)} from a key field holding nothing`);
    assert.notEqual(snackbarSaid(), SUCCESS_LINE, 'the control reported a copied key from a key field holding nothing');

    assertSaidNotCopied('the press over an empty key field on a page with a clipboard');
  } finally {
    restore();
  }
});
