// the API key the row can draw for the operator: what it draws, where the value lands, what the copy
// control copies after it, and what the page says about it. The row is read open, the way the operator
// reaches it, and the bytes the draw goes through are the test's own: the row draws through the browser's
// crypto object, so that is what the test hands a script.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-apikeys.js';

import { connectFields, fieldMessage, findAll, texts } from './dom-shim.js';

// The form a version-4 UUID takes: eight, four, four, four, then twelve lower-case hexadecimal digits, the
// third group starting with the version 4 and the fourth starting with one of 8, 9, a, or b.
const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

// The two keys the byte scripts below spell out. Each is sixteen bytes that already carry the version and
// variant nibbles a version-4 UUID fixes, so the key is those bytes read as hexadecimal and cut into the
// UUID's five groups, with nothing changed on the way.
const DRAWN_KEY_BYTES = [0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0x4c, 0xde, 0x8f, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd];
const ANOTHER_KEY_BYTES = [0x10, 0x21, 0x32, 0x43, 0x54, 0x65, 0x47, 0x87, 0x88, 0xa9, 0xba, 0xcb, 0xdc, 0xed, 0xfe, 0xef];

const DRAWN_KEY = uuidForm(DRAWN_KEY_BYTES);
const ANOTHER_KEY = uuidForm(ANOTHER_KEY_BYTES);

// The section's two rows: the row the readings press, and a row whose key no script here ever draws, so a
// drawn value is never a duplicate of it by accident.
const ROWS = [
  { id: 1, order: 1, key: 'key-one', ident: 'Dispatch', systems: '*', disabled: false },
  { id: 2, order: 2, key: 'key-two', ident: 'Gateway', systems: '*', disabled: false },
];

// the row the readings press in those rows, and the row that already carries DRAWN_KEY: the duplicate
// reading needs the key another row holds.
const DUPLICATE_ROWS = [
  { id: 1, order: 1, key: DRAWN_KEY, ident: 'Dispatch', systems: '*', disabled: false },
  { id: 2, order: 2, key: 'key-two', ident: 'Gateway', systems: '*', disabled: false },
];

// the transport the readings watch: no server, and every request the page sends is recorded. The
// response answers with what was sent, the way the server answers a saved section.
const requests = [];

globalThis.fetch = async (url, options = {}) => {
  const sent = options.body ? JSON.parse(options.body) : null;

  requests.push({ url, method: options.method ?? 'GET', sent });

  return { ok: true, status: 200, json: async () => sent };
};

// the shim lacks the clipboard: a fake the test owns, counting what it was handed.
const clipboardWrites = [];

globalThis.navigator.clipboard ??= {
  writeText: async (value) => {
    clipboardWrites.push(value);
  },
};

function seed(rows) {
  const { store } = getAdminContext();

  const baseline = rows.map((row) => ({ ...row }));

  // the page starts clean: a live region, a snackbar, or a clipboard write another reading made is not
  // this reading's, and neither is a request another reading sent.
  document.body.replaceChildren();
  requests.length = 0;
  clipboardWrites.length = 0;

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

  return { store, baseline };
}

function attach(tag) {
  const el = new (customElements.get(tag))();

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

// activation flips the open state and the row answers with a toggle event: the row is where the
// controls live, so a reading that cannot open it reads nothing.
function activate(row) {
  row.open = !row.open;
  row.dispatchEvent({ type: 'toggle', target: row, bubbles: false });
}

function fieldTitle(node) {
  return texts(findAll(node, (child) => child.tag === 'label'))[0];
}

function field(row, title) {
  return findAll(row, (node) => node.tag === 'rdio-form-field').find((node) => fieldTitle(node) === title);
}

function controlOf(row, title) {
  return findAll(field(row, title), (node) => ['input', 'textarea'].includes(node.tag))[0];
}

// the field's inline row: the input and the controls riding beside it.
function inlineRow(row, title) {
  return findAll(field(row, title), (node) => node.tag === 'div' && node.className?.split(/\s+/).includes('field-inline'))[0];
}

// the controls beside the key field, in the order the field presents them.
function inlineControls(row) {
  return findAll(inlineRow(row, 'Key'), (node) => node.tag === 'button');
}

function named(control) {
  return control?.getAttribute('aria-label') ?? '';
}

// the control that draws a new key, read by the name it answers under.
function generateControl(row) {
  return findAll(row, (node) => node.tag === 'button' && named(node) === 'Generate new key')[0] ?? null;
}

function copyControl(row) {
  return findAll(row, (node) => node.tag === 'button' && named(node) === 'Copy key to clipboard')[0] ?? null;
}

// the row at index, read fresh off the page, its draw control pressed.
function pressGenerate(el, index) {
  const row = rows(el)[index];

  const control = generateControl(row);

  assert.ok(control, 'the opened api key row carries no control beside the key that draws a new one');

  control.click();

  return rows(el)[index];
}

function keyFrom(el, index) {
  return controlOf(rows(el)[index], 'Key').value;
}

function typeIn(row, title, value) {
  const control = controlOf(row, title);

  control.value = value;
  control.dispatchEvent({ type: 'change' });
  connectFields(row);

  return control;
}

function markerOn(row) {
  return findAll(row, (node) => node.className?.split(/\s+/).includes('row-invalid') && node.hidden !== true).length;
}

// the section bar's own controls, re-read after every state the row changes: the bar rebuilds its controls
// when the slice answers, so a captured button is the bar's previous render.
function barControl(el, className) {
  const bar = findAll(el, (node) => node.tag === 'rdio-admin-section-bar')[0];

  assert.ok(bar, 'the api key section carries no save bar to answer its rows');

  bar.connectedCallback();

  return findAll(bar, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes(className))[0] ?? null;
}

function saveButton(el) {
  const save = barControl(el, 'save');

  assert.ok(save, 'the api key section bar offers no Save control');

  return save;
}

// The control the section itself offers to add a row: it carries the section's add path, and it sits in the
// section's own top row rather than in the save bar.
function addButton(el) {
  const add = findAll(el, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes('add'))[0];

  assert.ok(add, 'the api key section offers no control that adds a row');

  return add;
}

// A source the test scripts, one batch per scripted key: each call answers with the next key's sixteen
// bytes. Past the last scripted key it answers with no bytes at all, which is the shape of a source that
// cannot draw: a draw that asked for more than the reading scripted refuses instead of inventing a value or
// being asked again for ever. The source records every byte it handed, which is how a reading says which
// bytes a value was drawn from.
function keySource(keys) {
  const given = [];
  let batch = 0;

  return {
    given,
    getRandomValues(bytes) {
      const scripted = keys[batch++];

      if (!scripted) return new Uint8Array(0);

      for (let index = 0; index < bytes.length; index++) {
        bytes[index] = scripted[index];
        given.push(scripted[index]);
      }

      return bytes;
    },
  };
}

// A source that cannot draw at all: it answers nothing usable, which is what a browser without the
// entropy call gives the page.
function refusingSource() {
  return {
    getRandomValues() {
      throw new Error('the browser refused to draw');
    },
  };
}

// uuidForm reads sixteen bytes back as the canonical hexadecimal form of a version-4 UUID: five groups of
// eight, four, four, four, and twelve digits. The bytes these readings script already carry the two nibbles
// that form fixes, which each reading that uses them checks.
function uuidForm(bytes) {
  const hex = bytes.map((byte) => byte.toString(16).padStart(2, '0')).join('');

  return [hex.slice(0, 8), hex.slice(8, 12), hex.slice(12, 16), hex.slice(16, 20), hex.slice(20)].join('-');
}

function scriptedAsUUID(bytes) {
  assert.match(uuidForm(bytes), UUID_V4, `the reading scripted bytes that do not already carry the version and variant nibbles a version-4 UUID fixes: ${JSON.stringify(bytes)}`);
}

const realCrypto = Object.getOwnPropertyDescriptor(globalThis, 'crypto');

// The browser's crypto object, swapped for the test's source for the length of one reading: the row reads
// its entropy from the browser, exactly as every browser-side draw does. The source carries the entropy
// call and nothing else, which is the shape a plain-HTTP address on a LAN gives the page today.
async function drawn(source, reading) {
  Object.defineProperty(globalThis, 'crypto', { value: source, configurable: true, writable: true });

  try {
    return await reading();
  } finally {
    Object.defineProperty(globalThis, 'crypto', realCrypto);
  }
}

function settle() {
  return new Promise((resolve) => setImmediate(resolve));
}

// The key field carries one input, and the row's copy control beside it; the control that draws a new key
// rides to the right of that one, under its own name.
test('the api key field carries a draw control beside the control that copies the key', () => {
  scriptedAsUUID(DRAWN_KEY_BYTES);

  seed(ROWS);

  const el = attach('rdio-admin-apikeys');

  const row = rows(el)[0];

  activate(row);

  const inline = inlineRow(row, 'Key');

  assert.ok(inline, 'the key field carries no inline row to ride its controls on');

  const inputs = findAll(inline, (node) => node.tag === 'input');
  const controls = inlineControls(row);

  assert.equal(inputs.length, 1, 'the key field does not carry the one input its inline row is built around');
  assert.equal(controls.length, 2, 'the key field carries one control: the draw control beside the copy control is missing');

  const [copy, generate] = controls;

  assert.equal(named(copy), 'Copy key to clipboard', `the control these readings expect to keep its place answers under ${JSON.stringify(named(copy))}`);
  assert.ok(generate.className?.split(/\s+/).includes('rdio-icon-button'), 'the draw control beside the key is not the icon control these rows use');
  assert.equal(generate.type, 'button', 'the draw control beside the key submits the section instead of drawing a value');
  assert.equal(named(generate), 'Generate new key', `the draw control beside the key answers under ${JSON.stringify(named(generate))}, not a name saying it draws a new key`);
  assert.notEqual(named(generate), named(copy), 'the two controls beside the key field answer under the same name, so neither says what it does');
});

// A draw replaces what the row held, and the row keeps drawing: each press gives a different value.
test('pressing the draw control replaces the key, and pressing it again draws another', async () => {
  await drawn(keySource([DRAWN_KEY_BYTES, ANOTHER_KEY_BYTES]), () => {
    seed(ROWS);

    const el = attach('rdio-admin-apikeys');

    activate(rows(el)[0]);

    const held = keyFrom(el, 0);

    pressGenerate(el, 0);

    const first = keyFrom(el, 0);

    assert.notEqual(first, held, `the draw left the key the row already held in the field: ${JSON.stringify(first)}`);
    assert.match(first, UUID_V4, `the drawn value is not a key in the form this section's keys take: ${JSON.stringify(first)}`);
    assert.equal(first, DRAWN_KEY, 'the key the row drew did not come from the bytes the reading scripted');

    pressGenerate(el, 0);

    const second = keyFrom(el, 0);

    assert.notEqual(second, first, 'a second press drew the key the first press drew');
    assert.match(second, UUID_V4, `the second drawn value is not a key in the form this section's keys take: ${JSON.stringify(second)}`);
    assert.equal(second, ANOTHER_KEY, 'the second draw did not read the bytes after the ones the first draw used');
  });
});

// A drawn key is an edit like any other the row makes: it lands in the section's working copy, makes the
// section dirty, and reaches the server only when the operator saves.
test('a drawn api key lands in the working copy, makes the section dirty, and reaches the server only on Save', async () => {
  await drawn(keySource([DRAWN_KEY_BYTES, ANOTHER_KEY_BYTES]), async () => {
    const { store } = seed(ROWS);

    const el = attach('rdio-admin-apikeys');

    activate(rows(el)[0]);

    pressGenerate(el, 0);

    const drawnKey = keyFrom(el, 0);
    const slice = store.getState()['section:apikeys'];

    assert.equal(slice.working[0].key, drawnKey, 'the drawn key did not land in the section working copy the save sends');
    assert.equal(slice.dirty, true, 'the drawn key did not make the section dirty, so nothing offers it to the server');
    assert.deepEqual(slice.baseline, ROWS, `the draw changed the stored configuration before the operator saved: ${JSON.stringify(slice.baseline)}`);
    assert.equal(requests.length, 0, 'the draw reached the server before the operator saved');

    const save = saveButton(el);

    assert.equal(save.disabled, false, 'the Save control does not answer the rows the section now holds');

    save.click();

    await settle();

    assert.equal(requests.length, 1, `the save sent more than the section's own request: ${JSON.stringify(requests.map((request) => request.url))}`);

    const sent = requests[0];

    assert.equal(sent.url, '/api/admin/config/apikeys', 'the save did not go to the api key section endpoint it goes to today');
    assert.equal(sent.method, 'PUT', 'the save did not send the section the way it sends it today');
    assert.equal(sent.sent[0].key, drawnKey, 'the saved payload does not carry the key the row drew');
    assert.equal(sent.sent[1].key, 'key-two', 'the save changed the key the other row carries beside the drawn one');

    assert.equal(store.getState()['section:apikeys'].dirty, false, 'the section is still dirty after the server answered the save');
  });
});

// A key another row already carries is drawn again rather than written: the section would only refuse it.
// The field's message and the row's warning follow the value that lands.
test('a key another api key row carries is drawn again, and the row answers the value that lands', async () => {
  const source = keySource([DRAWN_KEY_BYTES, ANOTHER_KEY_BYTES]);

  await drawn(source, () => {
    const { store } = seed(DUPLICATE_ROWS);

    const el = attach('rdio-admin-apikeys');

    activate(rows(el)[1]);

    // the operator types the key the other row already carries, and leaves the field: the message and the
    // warning appear, and Save stops answering the rows.
    typeIn(rows(el)[1], 'Key', DRAWN_KEY);

    const stale = rows(el)[1];

    assert.equal(fieldMessage(field(stale, 'Key')), 'Key is already defined', 'the row did not say its key was already defined');
    assert.equal(markerOn(stale), 1, 'the row carrying a duplicate key carries no warning');
    assert.equal(saveButton(el).disabled, true, 'Save answers the section while one of its rows carries a duplicate key');

    pressGenerate(el, 1);

    // the bytes the first draw was made of are the key another row holds: the draw answered a value the
    // section already held, and had to draw again.
    assert.equal(uuidForm(source.given.slice(0, 16)), DRAWN_KEY, 'the draw was never asked for the key another row carries, so this reading did not test a duplicate');

    const landed = keyFrom(el, 1);

    assert.notEqual(landed, DRAWN_KEY, `the row wrote the key another row already carries: ${JSON.stringify(landed)}`);
    assert.equal(landed, ANOTHER_KEY, 'the value the row drew after the duplicate is not the one the reading scripted next');

    const row = rows(el)[1];

    assert.equal(fieldMessage(field(row, 'Key')), '', 'the row still says its key is already defined, beside a value that is not one');
    assert.equal(markerOn(row), 0, 'the row still carries its warning beside a value that is not a duplicate');
    assert.equal(store.getState()['section:apikeys'].invalid, false, 'the section still marks itself invalid beside rows that are not invalid');
    assert.equal(saveButton(el).disabled, false, 'the Save control does not answer the rows the section now holds');
  });
});

// The copy control is the control the operator reaches for the key they just drew: it copies what the field
// now holds, and the row stays the row it was.
test('the copy control copies the key the draw control just wrote, with the row left open', async () => {
  await drawn(keySource([DRAWN_KEY_BYTES]), async () => {
    const { store } = seed(ROWS);

    const el = attach('rdio-admin-apikeys');

    const opened = rows(el)[0];

    activate(opened);

    pressGenerate(el, 0);

    const drawnKey = keyFrom(el, 0);

    const copy = copyControl(rows(el)[0]);

    assert.ok(copy, 'the opened row carries no control that copies its key');

    copy.click();

    await settle();

    assert.deepEqual(clipboardWrites, [drawnKey], `the clipboard was handed ${JSON.stringify(clipboardWrites)}, not the key the draw control just wrote`);
    assert.equal(openRows(el).length, 1, 'the draw or the copy changed which row is open');
    assert.equal(rows(el)[0], openRows(el)[0], 'the row that drew and copied its key is no longer the open row');
    assert.equal(store.getState()['section:apikeys'].working[0].key, drawnKey, 'copying the drawn key committed something else beside it');
    assert.equal(requests.length, 0, 'copying the drawn key reached the server');
  });
});

// The section's add control gives the new row its key, and that is the control a plain-HTTP address on a
// LAN leaves without the call it reaches for today: the key has to come from the entropy call the address
// does answer.
test('the add control delivers a version-4 key when the browser offers no randomUUID call', async () => {
  await drawn(keySource([DRAWN_KEY_BYTES]), () => {
    scriptedAsUUID(DRAWN_KEY_BYTES);

    assert.equal(typeof globalThis.crypto.randomUUID, 'undefined', 'the reading did not put the add control in front of a browser without the randomUUID call');

    const { store } = seed(ROWS);

    const el = attach('rdio-admin-apikeys');

    const add = addButton(el);

    let key = null;

    try {
      add.click();

      key = keyFrom(el, 0);
    } catch (failure) {
      assert.fail(`the section's add control could not deliver a key to the row it added: ${failure.message}`);
    }

    assert.match(key, UUID_V4, `the added row's key is not in the form a version-4 UUID takes: ${JSON.stringify(key)}`);
    assert.equal(key, DRAWN_KEY, 'the key the add control delivered did not come from the bytes the reading scripted');
    assert.equal(store.getState()['section:apikeys'].working[0].key, DRAWN_KEY, 'the key the add control drew did not land in the section working copy');
    assert.equal(store.getState()['section:apikeys'].working[1].key, 'key-one', 'the added row re-keyed the row it was added above');
  });
});

// A draw the browser cannot answer is refused, not filled in with something weaker: the row keeps the key
// it held and the section keeps the state it was in.
test('when the source cannot draw, the row writes nothing and the section stays as it was', async () => {
  await drawn(refusingSource(), () => {
    const { store } = seed(ROWS);

    const el = attach('rdio-admin-apikeys');

    activate(rows(el)[0]);

    const held = keyFrom(el, 0);

    pressGenerate(el, 0);

    assert.equal(keyFrom(el, 0), held, 'the row wrote a value the source could not draw');
    assert.equal(store.getState()['section:apikeys'].working[0].key, held, 'the section holds a value the source could not draw');
    assert.equal(store.getState()['section:apikeys'].dirty, false, 'a refused draw made the section dirty');
    assert.equal(requests.length, 0, 'a refused draw reached the server');

    const said = document.querySelector('rdio-live-region')?.textContent.trim() ?? '';

    assert.match(said, /api key|key/i, `the page stayed silent about a key it could not draw: ${JSON.stringify(said)}`);
    assert.match(said, /could not|unable/i, `the page said something about the key it drew without saying it could not draw one: ${JSON.stringify(said)}`);
  });
});
