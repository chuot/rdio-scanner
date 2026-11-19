// row-list sections open one row at a time; readings come from the rendered rows
// rows are driven by the toggle event: the shim carries no keyboard or layout.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-access.js';
import '../presentation/admin/rdio-admin-apikeys.js';
import '../presentation/admin/rdio-admin-dirwatch.js';
import '../presentation/admin/rdio-admin-downstreams.js';
import '../presentation/admin/rdio-admin-groups.js';
import '../presentation/admin/rdio-admin-tags.js';

import { connectFields, fieldMessage, findAll, texts } from './dom-shim.js';

// each seeded list carries one invalid row, so the collapsed row's warning
// marker is read beside a valid one
const GROUPS = [
  { id: 1, order: 1, label: 'Dispatch' },
  { id: 2, order: 2, label: '' },
  { id: 3, order: 3, label: 'Console' },
];

const TAGS = [
  { id: 4, order: 1, label: 'Fire' },
  { id: 5, order: 2, label: '' },
  { id: 6, order: 3, label: 'Car Talk' },
];

const ACCESS = [
  { id: 7, order: 1, code: 'front-gate', ident: 'Front desk', systems: '*' },
  { id: 8, order: 2, code: '', ident: 'Gate house', systems: '*' },
  { id: 9, order: 3, code: 'shop-line', ident: 'Shop door', systems: '*' },
];

const APIKEYS = [
  { id: 10, order: 1, disabled: false, key: 'key-one', ident: 'Recorder', systems: '*' },
  { id: 11, order: 2, disabled: false, key: 'key-two', ident: '', systems: '*' },
  { id: 12, order: 3, disabled: false, key: 'key-three', ident: 'Booth line', systems: '*' },
];

const DOWNSTREAMS = [
  { id: 13, order: 1, disabled: false, url: 'https://one.example/api', apikey: 'key-one', systems: '*' },
  { id: 14, order: 2, disabled: false, url: '', apikey: 'key-two', systems: '*' },
  { id: 15, order: 3, disabled: false, url: 'https://three.example/api', apikey: 'key-three', systems: '*' },
];

const DIRWATCH = [
  { id: 16, order: 1, delay: 2000, deleteAfter: true, directory: '/var/record/metro', disabled: false, extension: 'mp3', frequency: 1, mask: '#SYS_#TG', type: 'default' },
  { id: 17, order: 2, delay: 2000, deleteAfter: true, directory: '', disabled: false, extension: 'mp3', frequency: 1, mask: '#SYS_#TG', type: 'default' },
  { id: 18, order: 3, delay: 2000, deleteAfter: true, directory: '/var/record/county', disabled: false, extension: 'mp3', frequency: 1, mask: '#SYS_#TG', type: 'default' },
];

const SYSTEMS = [
  { id: 1, order: 1, label: 'Metro', systemRef: 1, sites: [], talkgroups: [{ id: 11, talkgroupRef: 11, label: 'Fire dispatch' }], units: [] },
  { id: 2, order: 2, label: 'County', systemRef: 2, sites: [], talkgroups: [{ id: 12, talkgroupRef: 12, label: 'County dispatch' }], units: [] },
];

// The one alert the audition readings pick. The sequence carries the shape the audio schedules.
const ALERTS = { storm: [{ begin: 0, end: 0.04, frequency: 440, type: 'square' }] };

// each section: saved slice, panel key, collapsed identity words,
// the new row's fallback identity, and the field the readings reveal in
const SECTIONS = [
  {
    name: 'groups', tag: 'rdio-admin-groups', key: 'section:groups', prefix: 'grp', seedRows: GROUPS,
    titleOf: (row) => row.label || 'NewGroup', addTitle: 'NewGroup',
    field: 'Label', message: 'Label is required', audition: true,
  },
  {
    name: 'tags', tag: 'rdio-admin-tags', key: 'section:tags', prefix: 'tag', seedRows: TAGS,
    titleOf: (row) => row.label || 'NewTag', addTitle: 'NewTag',
    field: 'Label', message: 'Label is required', audition: true,
  },
  {
    name: 'access', tag: 'rdio-admin-access', key: 'section:access', prefix: 'acc', seedRows: ACCESS,
    titleOf: (row) => row.ident || 'NewAccess', addTitle: 'NewAccess',
    field: 'Code', message: 'Code is required',
  },
  {
    name: 'api key', tag: 'rdio-admin-apikeys', key: 'section:apikeys', prefix: 'api', seedRows: APIKEYS,
    titleOf: (row) => row.ident || 'NewAPIKey', addTitle: 'NewAPIKey',
    field: 'Key', message: 'Key is required',
  },
  {
    name: 'downstream', tag: 'rdio-admin-downstreams', key: 'section:downstreams', prefix: 'dwn', seedRows: DOWNSTREAMS,
    titleOf: (row) => row.url || 'NewDownstream', addTitle: 'NewDownstream',
    field: 'URL', message: 'URL is required',
  },
  {
    name: 'dirwatch', tag: 'rdio-admin-dirwatch', key: 'section:dirwatch', prefix: 'dw', seedRows: DIRWATCH,
    titleOf: (row) => row.directory || 'New dirwatch', addTitle: 'New dirwatch',
    field: 'Directory', message: 'Directory is required',
  },
];

function slice(baseline) {
  return { baseline, working: structuredClone(baseline), dirty: false, invalid: false, conflict: false };
}

// the store as the six sections expect it: seeded list, the systems the access
// dialog and dirwatch picks read, and the alerts the audition lists carry
function seed(section, alerts = {}) {
  const store = getAdminContext().store;

  const state = { auth: { authenticated: true, docker: false, passwordNeedChange: false }, alerts, 'section:systems': slice(SYSTEMS) };

  for (const other of SECTIONS) {
    state[other.key] = other.key === section.key ? slice(section.seedRows) : slice([]);
  }

  store.setState(state);

  return store;
}

function attach(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

// rows on the page, in section order, keyed the way the section names them
// (an expanded section carries none, which is what these readings catch)
function rows(el) {
  return findAll(el, (node) => node.tag === 'details' && node.dataset.key !== undefined);
}

// open rows are read from the rendered page, not the section's map
function openRows(el) {
  return rows(el).filter((row) => row.open);
}

function rowTitle(row) {
  return texts(findAll(row, (node) => node.className?.split(/\s+/).includes('row-title')))[0] ?? '';
}

function titles(el) {
  return rows(el).map(rowTitle);
}

function summaries(row) {
  return findAll(row, (node) => node.tag === 'summary');
}

// The Move up and Move down buttons the collapsed row carries beside its handle, in the order the
// row presents them.
function moveButtons(row) {
  return findAll(row, (node) => node.tag === 'button' && node.dataset?.move !== undefined);
}

// activation flips open state and the row answers with a toggle event;
// the event lands on the row, it does not bubble, so handlers sit on the row
function activate(row) {
  row.open = !row.open;
  row.dispatchEvent({ type: 'toggle', target: row, bubbles: false });
}

function control(el, className) {
  return findAll(el, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes(className))[0];
}

// a field answers to its name and its inner control: inner labels, or captions
// beside the control, depending on the section
function fieldTitle(node) {
  return texts(findAll(node, (child) => child.tag === 'label'))[0]
    ?? findAll(node.parent, (child) => child.className?.split(/\s+/).includes('field-title'))[0]?.textContent;
}

function field(row, title) {
  return findAll(row, (node) => node.tag === 'rdio-form-field').find((node) => fieldTitle(node) === title);
}

function controlOf(row, title) {
  return findAll(field(row, title), (node) => ['input', 'textarea', 'rdio-select'].includes(node.tag))[0];
}

// clearing a field then leaving it shows the message and warning icon
function revealEmptyField(row, title) {
  const input = controlOf(row, title);

  input.value = '';
  input.dispatchEvent({ type: 'change' });
  connectFields(row);

  return input;
}

// The operator types a value into the field and stays in it.
function typeIn(row, title, value) {
  const input = controlOf(row, title);

  input.value = value;
  input.dispatchEvent({ type: 'change' });
  connectFields(row);

  return input;
}

function markerOn(row) {
  return findAll(row, (node) => node.className?.split(/\s+/).includes('row-invalid')).length;
}

// the server replaces rows; the section rebuilds from the state the operator left
function pushRows(store, key, rows) {
  const current = store.getState()[key];

  store.setState({
    [key]: {
      ...current,
      baseline: rows.map((row) => ({ ...row })),
      working: rows.map((row) => ({ ...row })),
      dirty: false,
      invalid: false,
      conflict: false,
    },
  });
}

// rows carry by index, forEach, and iteration: no filter, map, or find;
// the shim returns an array, so this emulates the browser shape
function asBrowserRowList(found) {
  return {
    length: found.length,
    item: (i) => found[i] ?? null,
    forEach: (fn, thisArg) => found.forEach(fn, thisArg),
    [Symbol.iterator]: () => found[Symbol.iterator](),
  };
}

function listTheBrowserHands(el) {
  const read = el.querySelectorAll.bind(el);

  el.querySelectorAll = (selector) => {
    const found = read(selector);

    return selector.includes('details[data-key]') ? asBrowserRowList(found) : found;
  };

  return el;
}

// The sentence the page says about a row that changed its place in the list.
function announcedSentence() {
  return document.querySelector('rdio-live-region')?.textContent.trim() ?? '';
}

// a pointer drag: the shim delivers the dragstart and drop events, no real gesture
function dragTo(from, to) {
  from.dispatchEvent({ type: 'dragstart', target: from, bubbles: true, dataTransfer: {} });
  to.dispatchEvent({ type: 'drop', target: to, bubbles: true, dataTransfer: {}, preventDefault: () => {} });
}

// the shim lacks the clipboard and the audio context: fakes the test owns,
// counting what was asked of them
const clipboardWrites = [];

globalThis.navigator.clipboard ??= {
  writeText: async (value) => {
    clipboardWrites.push(value);
  },
};

const audio = { contexts: 0, oscillators: 0 };

class FakeAudioContext {
  constructor() {
    audio.contexts++;
    this.destination = {};
  }

  get currentTime() {
    return 0;
  }

  createGain() {
    return { gain: { value: 0 }, connect() {} };
  }

  createOscillator() {
    audio.oscillators++;

    return { connect() {}, frequency: {}, type: '', onended: null, start() {}, stop() {} };
  }
}

globalThis.AudioContext = FakeAudioContext;

// dialog answers are promises: the value lands in a microtask, then commit rebuilds
async function settleMicrotasks() {
  for (let step = 0; step < 10; step++) await Promise.resolve();
}

// rows are panels that start closed; the collapsed row shows identity, move
// controls, and a warning marker when the row breaks the section's rule
// fields stay hidden until the summary is activated
for (const section of SECTIONS) {
  test(`the ${section.name} section shows its rows closed, named, and marked when invalid`, () => {
    seed(section);

    const el = attach(section.tag);
    const on = rows(el);

    assert.equal(on.length, 3, `the ${section.name} section builds its three rows as panels the operator opens`);
    assert.equal(openRows(el).length, 0, 'the list starts with every row collapsed');

    section.seedRows.forEach((row, index) => {
      const panel = on[index];
      const invalid = index === 1;

      assert.equal(panel.tag, 'details', `the ${section.name} row is a panel with a summary the operator activates`);
      assert.match(
        panel.dataset.key,
        new RegExp(`^${section.prefix}:id\\d+$`),
        `the ${section.name} row is remembered by a key of its own, not by its place in the list`,
      );
      assert.ok(panel.dataset.drag !== undefined, 'the closed row still carries the handle the pointer drags it by');
      assert.equal(summaries(panel).length, 1, 'the closed row presents the summary line the operator opens it with');
      assert.equal(rowTitle(panel), section.titleOf(row), 'the closed row shows the identity the operator reads it by');
      assert.equal(panel.open, false, `the ${section.name} row reports itself collapsed`);

      const buttons = moveButtons(panel);

      assert.equal(buttons.length, 2, 'the closed row carries the Move controls the keyboard reorders it with');
      assert.equal(buttons[0].getAttribute('aria-label'), `Move up — ${section.titleOf(row)}`, 'the Move control names the row it moves in the row\'s own words');
      assert.equal(buttons[0].disabled, index === 0, 'the first row has nowhere to move up to, and says so');
      assert.equal(buttons[1].disabled, index === 2, 'the last row has nowhere to move down to, and says so');

      assert.equal(markerOn(panel), invalid ? 1 : 0, `the ${invalid ? 'invalid' : 'valid'} ${section.name} row shows ${invalid ? 'it is invalid' : 'no invalid marker'} while collapsed`);

      assert.ok(field(panel, section.field), `the collapsed panel carries the ${section.field.toLowerCase()} field the operator fills in when it is open`);
    });
  });
}

// one open at a time: the section mounts the shared row-open rule
// proven in domain-row-open-rule.test.js
for (const section of SECTIONS) {
  test(`opening a second ${section.name} row closes the first and leaves one row open`, () => {
    seed(section);

    const el = attach(section.tag);

    assert.equal(rows(el).length, 3, `the ${section.name} section shows three rows`);
    assert.equal(openRows(el).length, 0, 'the section opens with every row collapsed');

    const [first, second] = rows(el);

    activate(first);

    assert.equal(first.open, true, 'the row the operator activated is open');
    assert.equal(openRows(el).length, 1, 'the section shows one row open');

    activate(second);

    assert.equal(first.open, false, 'the row the operator opened first is collapsed');
    assert.equal(second.open, true, 'the row the operator opened last is open');
    assert.equal(openRows(el).length, 1, `the ${section.name} list holds exactly one open row`);
  });
}

// the open row survives a rebuild: never two open, never a phantom one
for (const section of SECTIONS) {
  test(`a ${section.name} rebuild with one row open shows exactly one open row`, () => {
    const store = seed(section);

    const el = attach(section.tag);

    assert.equal(rows(el).length, 3);

    const [first, second] = rows(el);
    const secondTitle = rowTitle(second);

    activate(first);
    activate(second);

    pushRows(store, section.key, store.getState()[section.key].working.map((row) => ({ ...row, order: row.order })));

    assert.equal(openRows(el).length, 1, 'the rebuild reproduces exactly one open row');
    assert.equal(openRows(el)[0], rows(el)[1], 'the rebuild keeps the row the operator opened open');
    assert.equal(rowTitle(rows(el)[1]), secondTitle);
  });
}

// calling filter reads zero rows and closes the row the operator left
for (const section of SECTIONS) {
  test(`a ${section.name} row left open on the page a browser hands the section stays open across the rebuild`, () => {
    const store = seed(section);

    const el = attach(section.tag);

    assert.equal(rows(el).length, 3);

    const [first, second] = rows(el);

    activate(first);
    activate(second);

    assert.equal(openRows(el).length, 1);

    listTheBrowserHands(el);

    pushRows(store, section.key, store.getState()[section.key].working.map((row) => ({ ...row })));

    assert.equal(openRows(el).length, 1, 'the rebuild read the open row off a NodeList, as the operator\'s browser hands it');
    assert.equal(openRows(el)[0], rows(el)[1], 'the rebuilt row the operator left open is the one row open');
  });
}

// the add control opens the new row alone, closing the row the operator had open
for (const section of SECTIONS) {
  test(`the ${section.name} add control opens the row it creates and closes the row the operator had open`, () => {
    seed(section);

    const el = attach(section.tag);

    assert.equal(rows(el).length, 3);

    const opened = rows(el)[0];
    const openedTitle = rowTitle(opened);

    activate(opened);

    assert.equal(openRows(el).length, 1, `an existing ${section.name} row is open before the add control is pressed`);

    control(el, 'add').click();
    connectFields(el);

    const all = rows(el);

    assert.equal(all.length, 4, 'the add control created a row');

    const fresh = all[0];

    assert.equal(fresh.open, true, 'the row the add control created is open');
    assert.match(fresh.dataset.key, new RegExp(`^${section.prefix}:new\\d+$`), 'the new row is remembered by the mark the client gave it, not by its place at the top of the list');
    assert.equal(rowTitle(fresh), section.addTitle, 'the new row shows the identity its section gives a row nobody has filled in');
    assert.equal(markerOn(fresh), 1, 'the new row says it needs a value while it is open');
    assert.equal(openRows(el).length, 1, 'the new row is the one open row the section shows');
    assert.equal(openRows(el)[0], fresh);

    assert.equal(all[1].open, false, 'the row the operator had open collapsed when the new row opened');
    assert.equal(rowTitle(all[1]), openedTitle, 'the row the operator had open is still on the page');
  });
}

// shrinking is presentation only: opening and closing writes nothing
// to the section's working copy
for (const section of SECTIONS) {
  test(`opening and closing ${section.name} rows changes nothing the section would save`, () => {
    const store = seed(section);

    const el = attach(section.tag);

    assert.equal(rows(el).length, 3);

    const before = store.getState()[section.key];
    const working = before.working;
    const snapshot = JSON.stringify(working);

    for (const row of rows(el)) activate(row);

    const after = store.getState()[section.key];

    assert.equal(after.working, working, 'the working copy is the same object: opening a row commits nothing');
    assert.equal(JSON.stringify(after.working), snapshot, `no ${section.name} row's fields change when rows open and close`);
    assert.equal(after.dirty, before.dirty, 'the dirty flag answers the same as it did before the clicks');
    assert.equal(after.dirty, false, 'reading rows does not enable the save bar');

    assert.equal(rows(el).length, 3, `the ${section.name} rows the operator is working on stay on the page`);
    assert.equal(openRows(el).length, 1, 'the last row the operator opened is the one row open');
  });
}

// a revealed message and the invalid marker survive collapse, back on reopen
for (const section of SECTIONS) {
  test(`a collapsed ${section.name} row keeps the message the operator revealed and its invalid marker`, () => {
    seed(section);

    const el = attach(section.tag);

    assert.equal(rows(el).length, 3);

    const [first, second] = rows(el);

    activate(first);
    revealEmptyField(first, section.field);

    assert.equal(fieldMessage(field(first, section.field)), section.message, 'the row the operator reached carries its message');
    assert.equal(markerOn(first), 1, 'the row the operator reached shows it is invalid');

    activate(second);

    assert.equal(first.open, false, 'opening another row collapses the row carrying the message');
    assert.equal(markerOn(first), 1, 'the warning icon rides the summary line of the collapsed row');
    assert.equal(openRows(el).length, 1, 'the collapsed row does not stay open to keep its message');

    activate(first);

    assert.equal(first.open, true, 'the operator opens the row again');
    assert.equal(fieldMessage(field(first, section.field)), section.message, 'the message is on the row when it is opened again');
    assert.equal(markerOn(first), 1, 'the invalid marker is on the row when it is opened again');
  });
}

// typing keeps the row open, the others closed, and the field in place
for (const section of SECTIONS) {
  test(`a keystroke in the open ${section.name} row closes nothing and leaves the field where it is`, () => {
    seed(section);

    const el = attach(section.tag);

    assert.equal(rows(el).length, 3);

    const second = rows(el)[1];

    activate(rows(el)[0]);
    activate(second);

    assert.equal(openRows(el).length, 1, 'the row the operator is about to type in is the one open row');

    typeIn(second, section.field, 'typed by the operator');

    assert.equal(second.open, true, 'the keystroke did not close the row the operator is typing in');
    assert.equal(openRows(el).length, 1, 'the keystroke closed nothing else either');
    assert.equal(openRows(el)[0], second);
    assert.equal(rows(el)[1], second, 'the row stayed on the page: the operator is still standing in it');
    assert.equal(controlOf(rows(el)[1], section.field).value, 'typed by the operator', 'the field the operator typed in is the field the page shows');
    assert.equal(rows(el)[0].open, false, 'the row the operator opened before is still collapsed');
  });
}

// keyboard Enter and Space produce the same activation as a click
// and every summary reports its row's expanded state
for (const section of SECTIONS) {
  test(`activating a ${section.name} summary the way the keyboard does closes the others, and every row says whether it is expanded`, () => {
    seed(section);

    const el = attach(section.tag);

    assert.equal(rows(el).length, 3);

    const [first, second, third] = rows(el);

    activate(first);
    activate(second);
    activate(third);

    assert.equal(openRows(el).length, 1, 'the keyboard\'s Enter left exactly one row open');
    assert.equal(openRows(el)[0], third, 'it is the row the keyboard last activated');

    for (const row of rows(el)) {
      assert.equal(summaries(row).length, 1, 'every row presents the summary the keyboard activates');
      assert.equal(row.open, row === third, `the ${rowTitle(row)} ${section.name} row states whether it is expanded`);
    }

    activate(third);

    assert.equal(openRows(el).length, 0, 'closing the open row records its own state and closes nothing else');

    activate(third);

    assert.equal(openRows(el).length, 1, 'the keyboard opens the same row again');
    assert.equal(openRows(el)[0], third);
  });
}

// The button of a row, named by its Move direction.
function moveButton(row, direction) {
  const button = moveButtons(row).find((candidate) => candidate.dataset.move === direction);

  assert.ok(button, `the row carries its Move ${direction} control`);

  return button;
}

// the Move buttons work on a collapsed row, and the move says the same sentence
for (const section of SECTIONS) {
  test(`pressing Move on a closed ${section.name} row reorders the list without opening it`, () => {
    const store = seed(section);

    const el = attach(section.tag);

    assert.equal(rows(el).length, 3);
    assert.equal(openRows(el).length, 0, 'every row is closed when the keyboard reaches for the Move button');

    const names = section.seedRows.map(section.titleOf);

    moveButton(rows(el)[2], 'up').click();

    assert.deepEqual(store.getState()[section.key].working.map((row) => row.id), [
      section.seedRows[0].id,
      section.seedRows[2].id,
      section.seedRows[1].id,
    ], `the closed ${section.name} row landed above the row it was behind`);
    assert.deepEqual(titles(el), [names[0], names[2], names[1]], 'the page shows the moved row in its new place');
    assert.equal(announcedSentence(), `${names[2]}, position 2 of 3`, 'the moved row says where it landed, in its own words');
    assert.equal(openRows(el).length, 0, 'moving a closed row did not open it');
  });
}

// a pointer drag reorders and announces like the Move button: one move
for (const section of SECTIONS) {
  test(`dragging a closed ${section.name} row reorders the list through the same sentence as the Move button`, () => {
    const store = seed(section);

    const el = attach(section.tag);

    assert.equal(rows(el).length, 3);
    assert.equal(openRows(el).length, 0);

    const names = section.seedRows.map(section.titleOf);

    dragTo(rows(el)[2], rows(el)[1]);

    assert.deepEqual(store.getState()[section.key].working.map((row) => row.id), [
      section.seedRows[0].id,
      section.seedRows[2].id,
      section.seedRows[1].id,
    ], `the dragged closed ${section.name} row landed through the same reorder the Move button goes through`);
    assert.deepEqual(titles(el), [names[0], names[2], names[1]], 'the page shows the dragged row in its new place');
    assert.equal(announcedSentence(), `${names[2]}, position 2 of 3`, 'the drag says the same sentence the Move button says');
    assert.equal(openRows(el).length, 0, 'dragging a closed row did not open it');
  });
}

// Move labels follow a renamed row, on a collapsed row too
// (Groups, Tags, and Dirwatch keep the collapsed identity in step)
for (const section of SECTIONS.filter((candidate) => ['groups', 'tags', 'dirwatch'].includes(candidate.name))) {
  test(`the collapsed ${section.name} row the operator renamed carries its new name on the controls that move it`, () => {
    seed(section);

    const el = attach(section.tag);

    assert.equal(rows(el).length, 3);

    const second = rows(el)[1];

    activate(second);
    typeIn(second, section.field, 'Renamed Row');

    assert.equal(rowTitle(second), 'Renamed Row', 'the opened row shows the name the operator typed');
    for (const button of moveButtons(second)) {
      assert.match(
        button.getAttribute('aria-label'),
        /^Move (up|down) — Renamed Row$/,
        `the ${section.name} Move control names the row by the name the page shows`,
      );
    }

    activate(rows(el)[0]);

    assert.equal(rowTitle(rows(el)[1]), 'Renamed Row', 'the renamed row shows the name it was given, now that it is collapsed');
    assert.equal(moveButtons(rows(el)[1])[0].getAttribute('aria-label'), 'Move up — Renamed Row', 'the collapsed renamed row still names itself on its Move control');
  });
}

// the Dirwatch type drives the row's field set: the rebuild keeps the row open
test('changing an open dirwatch row\'s type rebuilds its fields and keeps the row open', () => {
  const store = seed(SECTIONS.find((section) => section.name === 'dirwatch'));

  const el = attach('rdio-admin-dirwatch');

  assert.equal(rows(el).length, 3);

  const first = rows(el)[0];

  activate(first);

  assert.ok(controlOf(first, 'Mask'), 'the default dirwatch row carries the mask field its type uses');

  controlOf(first, 'Type').dispatchEvent(new CustomEvent('change', { detail: 'dsdplus' }));

  assert.equal(openRows(el).length, 1, 'the rebuild kept exactly one row open');
  assert.equal(openRows(el)[0], rows(el)[0], 'it is the row the operator changed');
  assert.equal(rowTitle(rows(el)[0]), '/var/record/metro');
  assert.equal(rows(el)[0].open, true, 'the rebuilt row reports itself open');
  assert.equal(field(rows(el)[0], 'Mask'), undefined, 'the rebuilt row dropped the mask field the dsd+ type does not use');
  assert.ok(controlOf(rows(el)[0], 'Extension'), 'the rebuilt row carries the fields the dsd+ type uses');
  assert.equal(store.getState()['section:dirwatch'].working[0].type, 'dsdplus', 'the type the operator picked is the row\'s type');
});

// the access code stays hidden until revealed; the control says which state it stands in, and the stored value is intact
test('the code reveal names and marks its state in both states and leaves the stored code untouched', () => {
  const store = seed(SECTIONS.find((section) => section.name === 'access'));

  const el = attach('rdio-admin-access');

  assert.equal(rows(el).length, 3);

  const first = rows(el)[0];

  activate(first);

  const codeInput = controlOf(first, 'Code');

  assert.equal(codeInput.type, 'password', 'the code starts hidden');

  // the toggle is the control carrying a pressed state: the code field rides two icon controls, and only
  // the one that shows the code says which way it stands.
  const reveal = findAll(first, (node) => node.tag === 'button' && node.getAttribute('aria-pressed') !== null)[0];

  assert.ok(reveal, 'the opened row carries no control that says whether the code is showing');

  // name, pressed state, and glyph are one state: read together, because a control whose words and whose
  // marks come apart says something the operator has to guess at.
  const revealState = () => ({
    name: reveal.getAttribute('aria-label'),
    pressed: reveal.getAttribute('aria-pressed'),
    glyph: findAll(reveal, (node) => node.className === 'material-icons')[0]?.textContent,
  });

  assert.deepEqual(
    revealState(),
    { name: 'Show code', pressed: 'false', glyph: 'visibility' },
    'while the code is hidden the reveal control does not say what pressing it would do',
  );

  reveal.click();

  assert.equal(codeInput.type, 'text', 'the control shows the code in the opened row');
  assert.deepEqual(
    revealState(),
    { name: 'Hide code', pressed: 'true', glyph: 'visibility_off' },
    'with the code showing the reveal control names the move it already made instead of the one it now makes',
  );

  reveal.click();

  assert.equal(codeInput.type, 'password', 'and hides it again');
  assert.deepEqual(
    revealState(),
    { name: 'Show code', pressed: 'false', glyph: 'visibility' },
    'after hiding the code again the reveal control carries neither its name nor its state',
  );
  assert.equal(openRows(el).length, 1, 'using a control inside the row did not close it');
  assert.equal(openRows(el)[0], first);
  assert.equal(store.getState()['section:access'].working[0].code, 'front-gate', 'showing the code commits nothing');
});

// Choose systems on the opened access row: the dialog value lands there
test('Choose systems on the opened access row lands on that row and keeps it open', async () => {
  const store = seed(SECTIONS.find((section) => section.name === 'access'));

  const el = attach('rdio-admin-access');

  assert.equal(rows(el).length, 3);

  const first = rows(el)[0];

  activate(first);

  const systemsButton = findAll(first, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes('choose-systems'))[0];

  assert.ok(systemsButton, 'the opened access row carries the control that picks the systems the code answers to');

  systemsButton.click();

  const dialog = document.querySelector('rdio-dialog');

  assert.ok(dialog, 'the selection dialog opened from the row');

  const everything = findAll(dialog, (node) => node.tag === 'input' && node.getAttribute('data-node') === 'everything')[0];

  assert.ok(everything?.checked, 'the row allows every system, and the dialog says so');

  everything.checked = false;
  everything.dispatchEvent({ type: 'change', target: everything, bubbles: true });

  const select = findAll(dialog, (node) => node.tag === 'button' && node.textContent.trim() === 'Select')[0];

  assert.ok(select, 'the dialog carries the answer the operator lands the choice with');

  select.click();
  await settleMicrotasks();

  const after = store.getState()['section:access'].working;

  assert.notEqual(after[0].systems, '*', 'the value the dialog answered with landed on the row the operator opened it from');
  assert.equal(after[1].systems, '*', 'the row the operator did not touch keeps its systems');

  assert.equal(openRows(el).length, 1, 'the row the answer landed on is still the one open row');
  assert.equal(rowTitle(openRows(el)[0]), 'Front desk');
});

// the copy control writes the opened key; nothing opens or commits
test('the copy control hands the opened api key row\'s key to the clipboard', async () => {
  const store = seed(SECTIONS.find((section) => section.name === 'api key'));

  const el = attach('rdio-admin-apikeys');

  assert.equal(rows(el).length, 3);

  const first = rows(el)[0];

  activate(first);

  const copy = findAll(first, (node) => node.tag === 'button' && node.getAttribute('aria-label') === 'Copy key to clipboard')[0];

  assert.ok(copy, 'the opened row carries the control that copies its key');

  copy.click();
  await settleMicrotasks();

  assert.ok(clipboardWrites.includes('key-one'), 'the clipboard was handed the key the row carries');
  assert.equal(openRows(el).length, 1, 'copying a key did not disturb which row is open');
  assert.equal(openRows(el)[0], first);
  assert.equal(store.getState()['section:apikeys'].working[0].key, 'key-one', 'copying a key commits nothing');
});

// the alert pick commits on the opened row; the audition uses fake audio
for (const section of SECTIONS.filter((candidate) => candidate.audition)) {
  test(`picking the audio alert on the opened ${section.name} row commits it and plays its tone`, () => {
    const store = seed(section, ALERTS);

    const el = attach(section.tag);

    assert.equal(rows(el).length, 3);

    const first = rows(el)[0];

    activate(first);

    const alertSelect = controlOf(first, 'Audio alert');

    assert.ok(alertSelect, 'the opened row carries the alert pick with its audition');

    const scheduled = audio.oscillators;

    alertSelect.dispatchEvent(new CustomEvent('change', { detail: 'storm' }));

    assert.equal(store.getState()[section.key].working[0].alert, 'storm', `the alert the operator picked rides the ${section.name} row`);
    assert.equal(audio.oscillators, scheduled + 1, 'the audition scheduled the tone the pick names');
    assert.equal(audio.contexts, 1, 'the audition plays through the one audio context the section holds');
    assert.equal(openRows(el).length, 1, 'picking an alert did not close the row');
    assert.equal(openRows(el)[0], rows(el)[0], 'the auditioned row is the row that was open');
  });
}

// The add control's name, in the words the operator presses.
test('the six list sections name their add control for the row it creates', () => {
  const labels = {
    'rdio-admin-access': 'New access',
    'rdio-admin-apikeys': 'New API key',
    'rdio-admin-dirwatch': 'New dir watch',
    'rdio-admin-downstreams': 'New downstream',
    'rdio-admin-groups': 'New Group',
    'rdio-admin-tags': 'New Tag',
  };

  for (const section of SECTIONS) {
    seed(section);

    const el = attach(section.tag);

    assert.equal(control(el, 'add').textContent, labels[section.tag], `the ${section.name} section's add control says which row it creates`);
  }
});
