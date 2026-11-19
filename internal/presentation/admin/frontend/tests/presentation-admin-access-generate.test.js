// the access code the row can draw for the operator: what it draws, where the value lands, and what the
// page says about it. The row is read open, the way the operator reaches it, and the bytes the draw goes
// through are the test's own: the row draws through the browser's crypto object, so that is what the test
// hands a script.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-access.js';

import { connectFields, fieldMessage, findAll, texts } from './dom-shim.js';

// The alphabet the server's own generator draws from (internal/domain/options.go's secretAlphabet): 26
// lowercase, 26 uppercase, 10 digits, and the five specials ! # $ % &. A code the page offers is one the
// server already accepts, so it carries exactly ten of these characters and nothing else.
const ACCESS_CODE_ALPHABET = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$%&';
const GENERATED_CODE = /^[a-zA-Z0-9!#$%&]{10}$/;

// The two codes the byte scripts below spell out. Every character of the alphabet is drawn by the byte
// naming its place in it, so a code is scripted as its own characters read as alphabet positions.
const DRAWN_CODE = 'abcdefghij';
const ANOTHER_CODE = 'klmnopqrst';

// The section's two rows: the row the readings press, and a row whose code no script here ever draws, so
// a drawn value is never a duplicate of it by accident.
const ROWS = [
  { id: 1, order: 1, code: 'front-gate', ident: 'Front desk', systems: '*' },
  { id: 2, order: 2, code: 'shop-line', ident: 'Shop door', systems: '*' },
];

// the row the readings press in those rows, and the row that already carries DRAWN_CODE: the duplicate
// reading needs the code another row holds, and only a code made of alphabet characters can be drawn.
const DUPLICATE_ROWS = [
  { id: 1, order: 1, code: DRAWN_CODE, ident: 'Front desk', systems: '*' },
  { id: 2, order: 2, code: 'front-gate', ident: 'Shop door', systems: '*' },
];

// the transport the readings watch: no server, and every request the page sends is recorded. The
// response answers with what was sent, the way the server answers a saved section.
const requests = [];

globalThis.fetch = async (url, options = {}) => {
  const sent = options.body ? JSON.parse(options.body) : null;

  requests.push({ url, method: options.method ?? 'GET', sent });

  return { ok: true, status: 200, json: async () => sent };
};

function seed(rows) {
  const { store } = getAdminContext();

  const baseline = rows.map((row) => ({ ...row }));

  // the page starts clean: a live region or a snackbar another test left on it is not this reading's,
  // and neither is a request another test sent.
  document.body.replaceChildren();
  requests.length = 0;

  store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    alerts: {},
    'section:access': {
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

// the controls beside the code field, in the order the field presents them.
function inlineControls(row) {
  return findAll(inlineRow(row, 'Code'), (node) => node.tag === 'button');
}

function named(control) {
  return control?.getAttribute('aria-label') ?? '';
}

// the control that draws a new code, read by the name it answers under.
function generateControl(row) {
  return findAll(row, (node) => node.tag === 'button' && named(node) === 'Generate new code')[0] ?? null;
}

// the row at index, read fresh off the page, its draw control pressed.
function pressGenerate(el, index) {
  const row = rows(el)[index];

  const control = generateControl(row);

  assert.ok(control, 'the opened access row carries no control beside the code that draws a new one');

  control.click();

  return rows(el)[index];
}

function codeValue(el, index) {
  return controlOf(rows(el)[index], 'Code').value;
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

// the section bar's own Save, re-read after every state the row changes: the bar rebuilds its controls
// when the slice answers, so a captured button is the bar's previous render.
function barControl(el, className) {
  const bar = findAll(el, (node) => node.tag === 'rdio-admin-section-bar')[0];

  assert.ok(bar, 'the access section carries no save bar to answer its rows');

  bar.connectedCallback();

  return findAll(bar, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes(className))[0] ?? null;
}

function saveButton(el) {
  const save = barControl(el, 'save');

  assert.ok(save, 'the access section bar offers no Save control');

  return save;
}

// A byte no even draw accepts: it stands for the values a draw throws away, and it fills the part of a
// batch a value never reaches.
const UNREAD_BYTE = 255;

// A source the test scripts, one batch per scripted code: each call answers with the next code spelled as
// the alphabet positions that draw it, and fills whatever else the call asked for with bytes no draw
// accepts. A draw reads only the bytes its value needs and starts a fresh batch for the next value, so one
// batch is one scripted value however many bytes a batch holds. Past the last scripted code it hands only
// unusable bytes, so a draw that kept asking runs out and refuses rather than being waited on. The source
// records every byte it handed, which is how a reading says which bytes a value was drawn from.
function codeSource(codes) {
  const given = [];
  let batch = 0;

  return {
    given,
    getRandomValues(bytes) {
      const scripted = batch < codes.length ? codes[batch] : '';

      batch++;

      for (let index = 0; index < bytes.length; index++) {
        const byte = index < scripted.length ? positionsOf(scripted[index]) : UNREAD_BYTE;

        bytes[index] = byte;
        given.push(byte);
      }

      return bytes;
    },
  };
}

// the byte that draws one character: every character of the alphabet sits at its own position in the span
// an even draw accepts.
function positionsOf(character) {
  const position = ACCESS_CODE_ALPHABET.indexOf(character);

  assert.ok(position >= 0, `the reading scripted ${JSON.stringify(character)}, which is outside the alphabet a drawn code is made of`);

  return position;
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

// charactersOf reads bytes back as the characters they draw, the way the server's alphabet does.
function charactersOf(bytes) {
  return bytes.map((byte) => ACCESS_CODE_ALPHABET[byte % ACCESS_CODE_ALPHABET.length]).join('');
}

const realCrypto = Object.getOwnPropertyDescriptor(globalThis, 'crypto');

// The browser's crypto object, swapped for the test's source for the length of one reading: the row reads
// its entropy from the browser, exactly as every browser-side draw does.
async function drawn(source, reading) {
  Object.defineProperty(globalThis, 'crypto', { value: source, configurable: true, writable: true });

  try {
    return await reading();
  } finally {
    Object.defineProperty(globalThis, 'crypto', realCrypto);
  }
}

// The code field carries one input, and the row's show/hide control beside it; the control that draws a
// new code rides to the right of that one, under its own name.
test('the access code field carries a draw control beside the control that shows the code', () => {
  seed(ROWS);

  const el = attach('rdio-admin-access');

  const row = rows(el)[0];

  activate(row);

  const inline = inlineRow(row, 'Code');

  assert.ok(inline, 'the code field carries no inline row to ride its controls on');

  const inputs = findAll(inline, (node) => node.tag === 'input');
  const controls = inlineControls(row);

  assert.equal(inputs.length, 1, 'the code field does not carry the one input its inline row is built around');
  assert.equal(controls.length, 2, 'the code field carries one control: the draw control beside the show/hide control is missing');

  const [reveal, generate] = controls;

  assert.ok(generate.className?.split(/\s+/).includes('rdio-icon-button'), 'the draw control beside the code is not the icon control these rows use');
  assert.equal(generate.type, 'button', 'the draw control beside the code submits the section instead of drawing a value');
  assert.equal(named(generate), 'Generate new code', `the draw control beside the code answers under ${JSON.stringify(named(generate))}, not a name saying it draws a new code`);
  assert.notEqual(named(generate), named(reveal), 'the two controls beside the code field answer under the same name, so neither says what it does');
});

// A draw replaces what the row held, and the row keeps drawing: each press gives a different value.
test('pressing the draw control replaces the code, and pressing it again draws another', async () => {
  await drawn(codeSource([DRAWN_CODE, ANOTHER_CODE]), () => {
    seed(ROWS);

    const el = attach('rdio-admin-access');

    const row = rows(el)[0];

    activate(row);

    const held = codeValue(el, 0);

    pressGenerate(el, 0);

    const first = codeValue(el, 0);

    assert.notEqual(first, held, `the draw left the code the row already held in the field: ${JSON.stringify(first)}`);
    assert.match(first, GENERATED_CODE, `the drawn value is not ten characters of the alphabet the server accepts: ${JSON.stringify(first)}`);
    assert.equal(first, DRAWN_CODE, 'the code the row drew did not come from the bytes the reading scripted');

    pressGenerate(el, 0);

    const second = codeValue(el, 0);

    assert.notEqual(second, first, 'a second press drew the code the first press drew');
    assert.match(second, GENERATED_CODE, `the second drawn value is not ten characters of the alphabet the server accepts: ${JSON.stringify(second)}`);
    assert.equal(second, ANOTHER_CODE, 'the second draw did not read the bytes after the ones the first draw used');
  });
});

// A drawn code is an edit like any other the row makes: it lands in the section's working copy, makes the
// section dirty, and reaches the server only when the operator saves.
test('a drawn access code lands in the working copy, makes the section dirty, and reaches the server only on Save', async () => {
  const source = codeSource([DRAWN_CODE, ANOTHER_CODE]);

  await drawn(source, async () => {
    const { store, baseline } = seed(ROWS);

    const el = attach('rdio-admin-access');

    activate(rows(el)[0]);

    pressGenerate(el, 0);

    const drawnCode = codeValue(el, 0);
    const slice = store.getState()['section:access'];

    assert.equal(slice.working[0].code, drawnCode, 'the drawn code did not land in the section working copy the save sends');
    assert.equal(slice.dirty, true, 'the drawn code did not make the section dirty, so nothing offers it to the server');
    assert.deepEqual(slice.baseline, ROWS, `the draw changed the stored configuration before the operator saved: ${JSON.stringify(slice.baseline)}`);
    assert.equal(requests.length, 0, 'the draw reached the server before the operator saved');

    const save = saveButton(el);

    assert.equal(save.disabled, false, 'the Save control does not answer the rows the section now holds');

    save.click();

    await new Promise((resolve) => setImmediate(resolve));

    assert.equal(requests.length, 1, `the save sent more than the section's own request: ${JSON.stringify(requests.map((request) => request.url))}`);

    const sent = requests[0];

    assert.equal(sent.url, '/api/admin/config/access', 'the save did not go to the access section endpoint it goes to today');
    assert.equal(sent.method, 'PUT', 'the save did not send the section the way it sends it today');
    assert.equal(sent.sent[0].code, drawnCode, 'the saved payload does not carry the code the row drew');
    assert.equal(sent.sent[1].code, 'shop-line', 'the save changed the code the other row carries beside the drawn one');

    assert.equal(store.getState()['section:access'].dirty, false, 'the section is still dirty after the server answered the save');
  });
});

// A code another row already carries is drawn again rather than written: the section would only refuse
// it. The field's message and the row's warning follow the value that lands.
test('a code another access row carries is drawn again, and the row answers the value that lands', async () => {
  const source = codeSource([DRAWN_CODE, ANOTHER_CODE]);

  await drawn(source, () => {
    const { store } = seed(DUPLICATE_ROWS);

    const el = attach('rdio-admin-access');

    activate(rows(el)[1]);

    // the operator types the code the other row already carries, and leaves the field: the message and
    // the warning appear, and Save stops answering the rows.
    typeIn(rows(el)[1], 'Code', DRAWN_CODE);

    const stale = rows(el)[1];

    assert.equal(fieldMessage(field(stale, 'Code')), 'Code is already defined', 'the row did not say its code was already defined');
    assert.equal(markerOn(stale), 1, 'the row carrying a duplicate code carries no warning');
    assert.equal(saveButton(el).disabled, true, 'Save answers the section while one of its rows carries a duplicate code');

    pressGenerate(el, 1);

    // the bytes the draw was made of are the held code read as alphabet positions: the draw answered a
    // value the section already held, and had to draw again.
    assert.equal(charactersOf(source.given.slice(0, DRAWN_CODE.length)), DRAWN_CODE, 'the draw was never asked for the code another row carries, so this reading did not test a duplicate');

    const landed = codeValue(el, 1);

    assert.notEqual(landed, DRAWN_CODE, `the row wrote the code another row already carries: ${JSON.stringify(landed)}`);
    assert.equal(landed, ANOTHER_CODE, 'the value the row drew after the duplicate is not the one the reading scripted next');

    const row = rows(el)[1];

    assert.equal(fieldMessage(field(row, 'Code')), '', 'the row still says its code is already defined, beside a value that is not one');
    assert.equal(markerOn(row), 0, 'the row still carries its warning beside a value that is not a duplicate');
    assert.equal(store.getState()['section:access'].invalid, false, 'the section still marks itself invalid beside rows that are not invalid');
    assert.equal(saveButton(el).disabled, false, 'the Save control does not answer the rows the section now holds');
  });
});

// A code the operator cannot read is a code they cannot hand to anyone.
test('the draw leaves the new code readable in the field rather than in the hidden one', async () => {
  await drawn(codeSource([DRAWN_CODE]), () => {
    seed(ROWS);

    const el = attach('rdio-admin-access');

    activate(rows(el)[0]);

    assert.equal(controlOf(rows(el)[0], 'Code').type, 'password', 'the code field no longer starts hidden');

    pressGenerate(el, 0);

    const input = controlOf(rows(el)[0], 'Code');

    assert.equal(input.type, 'text', 'the drawn code landed in the hidden field, where the operator cannot read it');
    assert.match(input.value, GENERATED_CODE, `the readable code is not ten characters of the alphabet the server accepts: ${JSON.stringify(input.value)}`);
  });
});

// The field is not the only answer the page gives: the sentence goes into the polite region the page
// already speaks through.
test('the page names the code it drew and says saving is what applies it', async () => {
  await drawn(codeSource([DRAWN_CODE]), () => {
    seed(ROWS);

    const el = attach('rdio-admin-access');

    activate(rows(el)[0]);

    pressGenerate(el, 0);

    const region = document.querySelector('rdio-live-region');

    assert.ok(region, 'the page said nothing about the code it drew');
    assert.equal(region.getAttribute('aria-live'), 'polite', 'the page spoke over what the operator was reading instead of beside it');

    const said = region.textContent.trim();

    assert.match(said, /access code/i, `the page did not name what it generated: ${JSON.stringify(said)}`);
    assert.match(said, /save/i, `the page did not say that saving is what applies the code it drew: ${JSON.stringify(said)}`);
  });
});

// A draw the browser cannot answer is refused, not filled in with something weaker: the row keeps the
// code it held and the section keeps the state it was in.
test('when the source cannot draw, the row writes nothing and the section stays as it was', async () => {
  await drawn(refusingSource(), () => {
    const { store } = seed(ROWS);

    const el = attach('rdio-admin-access');

    activate(rows(el)[0]);

    const held = codeValue(el, 0);

    pressGenerate(el, 0);

    assert.equal(codeValue(el, 0), held, 'the row wrote a value the source could not draw');
    assert.equal(store.getState()['section:access'].working[0].code, held, 'the section holds a value the source could not draw');
    assert.equal(store.getState()['section:access'].dirty, false, 'a refused draw made the section dirty');
    assert.equal(requests.length, 0, 'a refused draw reached the server');

    const said = document.querySelector('rdio-live-region')?.textContent.trim() ?? '';

    assert.match(said, /access code/i, `the page stayed silent about a code it could not draw: ${JSON.stringify(said)}`);
    assert.match(said, /could not|unable/i, `the page said something about the code it drew without saying it could not draw one: ${JSON.stringify(said)}`);
  });
});
