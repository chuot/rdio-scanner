// the control that stops the call a results row is playing, and the state the app is left in:
// the mode decides whether the operator has to ask for the live feed again, whether the walk to
// the next searched call steps on, and what the rows offer while the feed is held.

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

// Two systems to search, talkgroups the rows name, and no contact address so the help button
// stays off the page. The keypad beeps are left out: a beep would be handed to an audio device
// the shim does not have, and no reading here is about sound.
const CONFIG = {
  email: '',
  groups: { Dispatch: {}, Interop: {} },
  groupsData: [{ id: 1, label: 'Dispatch' }, { id: 2, label: 'Interop' }],
  tags: { Fire: { 1: [10, 11], 2: [20] } },
  tagsData: [{ id: 3, label: 'Fire' }],
  systems: [
    {
      id: 1,
      label: 'Metro',
      talkgroups: [
        { id: 10, label: 'Fire dispatch', groups: ['Dispatch'], tag: 'Fire' },
        { id: 11, label: 'Tac dispatch', groups: ['Dispatch'], tag: 'Fire' },
      ],
    },
    { id: 2, label: 'County', talkgroups: [{ id: 20, label: 'Sheriff', groups: ['Interop'], tag: 'Fire' }] },
  ],
};

// The searched calls, oldest first, in the shape a search answer carries them: the references the
// history stores, the time and length the columns show.
const ANSWER = [
  { id: 101, system: 1, talkgroup: 10, dateTime: new Date(2026, 9, 6, 9, 15).toISOString(), audioLength: 12 },
  { id: 102, system: 1, talkgroup: 11, dateTime: new Date(2026, 9, 6, 10, 30).toISOString(), audioLength: 30 },
  { id: 103, system: 2, talkgroup: 20, dateTime: new Date(2026, 9, 6, 11, 40).toISOString(), audioLength: 8 },
  { id: 104, system: 2, talkgroup: 20, dateTime: new Date(2026, 9, 6, 12, 5).toISOString(), audioLength: 21 },
];

// The frame a call arrives on, the way the server answers a call request: the references, the
// time, and the recording itself.
const recording = (id, system, talkgroup) => ({
  id,
  system,
  talkgroup,
  dateTime: new Date(2026, 9, 6, 9, 15).toISOString(),
  audioLength: 12,
  audio: { data: [0, 1, 2] },
});

// The shim's connection answers nothing and records nothing. These readings are about the search
// the panel asked for, the calls that answer asked for, and the mode and the rows the app is left
// with, so the connection is replaced with one that keeps what the app sends and answers the way
// the server does. The composition root wires one connection for the file, and each case clears
// the record before it drives the page.
let connection = null;

class RecordedConnection {
  constructor(url) {
    this.url = url;
    this.readyState = 1;
    this.sent = [];
    connection = this;
  }

  send(data) {
    this.sent.push(JSON.parse(data));
  }

  close() {}

  // The adapter opens the link, asks for the version and the configuration, and answers nothing
  // until the test hands it a frame. The test fires the opening itself so the frames the opening
  // sends land in the record before the reading starts.
  link() {
    this.onopen?.();
  }

  answer(command, payload, flag) {
    const frame = flag === undefined ? [command, payload] : [command, payload, flag];

    this.onmessage?.({ data: JSON.stringify(frame) });
  }

  // The frames that asked the server for a recording to play: the walk the app is stepping
  // through, read as the requests it made.
  playRequests(from = 0) {
    return this.sent.slice(from).filter((frame) => frame[0] === 'CAL' && frame[2] === 'play');
  }
}

globalThis.WebSocket = RecordedConnection;

// The app, on the page, with the search panel open on an answered list. The use-case closure
// outlives one case, so the mode and the hold are put back before each case drives the panel.
function searchScreen() {
  const { store, useCases } = getAppContext();

  document.body.replaceChildren();

  if (useCases.livefeedMode !== 'offline') useCases.livefeed();

  if (useCases.livefeedPaused) useCases.pause(false);

  store.setState({
    auth: { status: 'anonymous', denied: false },
    call: undefined,
    livefeedMode: 'offline',
    pause: false,
    playbackList: null,
    playbackListSeq: 0,
    queue: 0,
  });

  const el = document.createElement('rdio-app');

  const realSetTimeout = globalThis.setTimeout;
  const realSetInterval = globalThis.setInterval;

  // the promo and the display's clock are timers the shim does not model: they are handed to the
  // test during the render and left unrun until the reading is taken.
  globalThis.setTimeout = () => 0;
  globalThis.setInterval = () => 0;

  try {
    el.connectedCallback();
  } finally {
    globalThis.setTimeout = realSetTimeout;
    globalThis.setInterval = realSetInterval;
  }

  document.body.append(el);

  connection.link();
  connection.answer('CFG', CONFIG);
  connection.sent.length = 0;

  const panel = findAll(el, (node) => node.tag === 'rdio-search-panel')[0];

  panel.open();

  searchAnswered();

  return { el, panel, store, useCases, requests: (from) => connection.playRequests(from), sent: () => connection.sent };
}

// The search the operator ran, and the answer it got. The list is answered through the
// connection, the way a search request is answered, so the app holds the answer itself.
function searchAnswered() {
  connection.answer('LCL', {
    count: ANSWER.length,
    options: { sort: 1, limit: 200, offset: 0 },
    results: ANSWER.map((row) => ({ ...row })),
  });
}

function control(el, action) {
  const button = findAll(el, (node) => node.tag === 'button' && node.dataset.action === action)[0];

  assert.ok(button, `the control bar offers no ${action} control`);

  return button;
}

function classes(node) {
  return node.className.split(/\s+/).filter(Boolean);
}

// The control one row offers: the row-action button named by the call it stands on, whichever of
// the marks — play, stop, download — or the disabled loading mark the render put on it.
function actionFor(panel, id) {
  const button = findAll(panel.tbody, (node) =>
    node.tag === 'button' && ['play', 'stop', 'download'].some((mark) => node.dataset[mark] === String(id)))[0];

  assert.ok(button, `the results list offers no control for call ${id}`);

  return button;
}

function labelOf(button) {
  return button.getAttribute('aria-label') ?? '';
}

function playControls(panel) {
  return findAll(panel.tbody, (node) => node.tag === 'button' && labelOf(node) === 'Play');
}

// The row controls the operator can press: a control the render left out of reach carries the
// disabled mark, the same mark the loading row already carries.
function pressablePlayControls(panel) {
  return playControls(panel).filter((button) => button.attributes.disabled === undefined);
}

// The control each row carries, in the order the operator reads the list. The loading row's
// control names no call — the disabled mark is all it carries — so the rows are read by position
// rather than by the call a control is named for.
function rowControls(panel) {
  return findAll(panel.tbody, (node) => node.tag === 'tr')
    .map((row) => findAll(row, (node) => node.tag === 'td')[0])
    .map((cell) => (cell ? findAll(cell, (node) => node.tag === 'button')[0] : null))
    .filter((button) => button)
    .map((button) => `${labelOf(button)}${button.attributes.disabled === undefined ? '' : ' (out of reach)'}`);
}

// The cells that name a row: the date, time, length, system, talkgroup, and name the operator
// reads a call by. The control the row carries is left out, because what a row offers moves with
// the state the app is in and these readings are about which calls are on the list.
function rowLabels(panel) {
  return findAll(panel.tbody, (node) => node.tag === 'tr')
    .map((row) => findAll(row, (node) => node.tag === 'td').slice(1).map((cell) => cell.textContent).join('|'))
    .filter((line) => line.split('|')[3] !== '');
}

// A filter the operator changed: the value is put on the control and the change is handed to the
// form the way picking an option does.
function pick(panel, name, value) {
  const control = panel.form.elements[name];

  control.value = value;

  panel.form.dispatchEvent({ type: 'change', target: control, bubbles: true });
}

// A searched call the app is playing: the operator pressed its row, the server answered with the
// recording, and the call is the one the display and the rows stand on. The recording is handed
// over the connection, the way a played call arrives, rather than written into the state the page
// reads, so the walk, the queue number, and the mode the readings are taken from are the app's.
function playingCall(screen, id = 101) {
  const asked = connection.sent.length;

  screen.useCases.loadAndPlay(id);

  assert.equal(screen.sent().at(-1)[2], 'play', 'the row the operator pressed asked for no recording');

  const row = ANSWER.find((call) => call.id === id);

  connection.answer('CAL', recording(id, row.system, row.talkgroup), 'play');

  assert.equal(screen.store.getState().call?.id, id, 'the recording the server handed over never reached the page');

  return actionFor(screen.panel, id);
}

describe('presentation/listener/rdio-search-panel — stopping the searched call ends the searched playback', () => {
  it('a row stop leaves the app out of playback mode', () => {
    const screen = searchScreen();
    const stop = playingCall(screen);

    assert.equal(screen.useCases.livefeedMode, 'playback', 'the row the stop stands on is not a searched playback');

    stop.click();

    assert.equal(screen.useCases.livefeedMode, 'offline', `the stop left the app in ${screen.useCases.livefeedMode} mode`);
    assert.equal(screen.store.getState().livefeedMode, 'offline', 'the page still shows a mode the stop did not end');
  });

  it('a row stop leaves nothing waiting behind the call it stopped', () => {
    const screen = searchScreen();
    const stop = playingCall(screen);

    assert.ok(screen.store.getState().queue > 0, 'the searched playback had nothing ahead of the call being played');

    stop.click();

    assert.equal(screen.store.getState().queue, 0, 'calls the operator stopped are still counted as waiting');
  });

  it('a row stop takes the call it stopped off the display', () => {
    const screen = searchScreen();

    playingCall(screen).click();

    assert.equal(screen.store.getState().call, null, 'the call the operator stopped is still the call being played');
    assert.equal(findAll(screen.panel.tbody, (node) => node.tag === 'button' && labelOf(node) === 'Stop').length, 0,
      'a row still offers a stop for a call that is no longer playing');
  });

  it('the live feed control reads one mode after a row stop', () => {
    const screen = searchScreen();

    playingCall(screen).click();

    const livefeed = control(screen.el, 'livefeed');

    assert.deepEqual(classes(livefeed), ['rdio-button', 'off'],
      `the live feed control reads ${classes(livefeed).join(' ')} after the stop: two modes at once`);
  });

  it('one press of the live feed control brings the live feed back after a row stop', () => {
    const screen = searchScreen();

    playingCall(screen).click();

    control(screen.el, 'livefeed').click();

    assert.equal(screen.useCases.livefeedMode, 'online',
      `one press left the app in ${screen.useCases.livefeedMode} mode, so the live feed needed more than one press`);
    assert.ok(classes(control(screen.el, 'livefeed')).includes('on'), 'the live feed control does not read the live feed it is in');
  });

  it('skip next asks for no further searched call after a row stop', () => {
    const screen = searchScreen();

    playingCall(screen).click();

    const before = screen.sent().length;

    control(screen.el, 'skip').click();

    assert.deepEqual(screen.requests(before), [],
      `the walk stepped on to another searched call: ${JSON.stringify(screen.requests(before))}`);
  });

  it('a row stop leaves the searched answer on the results list', () => {
    const screen = searchScreen();
    const rows = rowLabels(screen.panel);

    playingCall(screen).click();

    assert.ok(screen.store.getState().playbackList, 'the answer the search gave was dropped with the call');
    assert.deepEqual(rowLabels(screen.panel), rows, 'the list the operator searched out is gone with the call');
  });

  it('a stop on a call the live feed reached leaves the live feed running', () => {
    const screen = searchScreen();

    screen.useCases.livefeed();

    const before = screen.sent().length;

    connection.answer('CAL', recording(102, 1, 11));

    assert.equal(screen.store.getState().call?.id, 102, 'the call that arrived never reached the page');

    actionFor(screen.panel, 102).click();

    assert.equal(screen.useCases.livefeedMode, 'online',
      `stopping a call the live feed reached left the app in ${screen.useCases.livefeedMode} mode`);
    assert.ok(classes(control(screen.el, 'livefeed')).includes('on'), 'the live feed control no longer reads the live feed');
    assert.deepEqual(screen.requests(before), [], 'stopping a live call asked the server for a recording');
  });
});

describe('presentation/listener/rdio-search-panel — a held feed holds the rows it shows', () => {
  it('no row offers a pressable play control while a searched playback is held', () => {
    const screen = searchScreen();

    playingCall(screen);

    const before = pressablePlayControls(screen.panel).length;

    control(screen.el, 'pause').click();

    const pressable = pressablePlayControls(screen.panel).length;
    const total = playControls(screen.panel).length;

    control(screen.el, 'pause').click();

    const resumed = pressablePlayControls(screen.panel).length;

    assert.deepEqual([before, pressable, resumed], [before, 0, before],
      `${pressable} of ${total} rows offer a play control the operator can press while the feed is held, of ${before} before the hold and ${resumed} after it`);
  });

  it('no row offers a pressable play control while the live feed is held', () => {
    const screen = searchScreen();

    control(screen.el, 'livefeed').click();

    const before = pressablePlayControls(screen.panel).length;

    control(screen.el, 'pause').click();

    const pressable = pressablePlayControls(screen.panel).length;
    const total = playControls(screen.panel).length;

    control(screen.el, 'pause').click();

    const resumed = pressablePlayControls(screen.panel).length;

    assert.deepEqual([before, pressable, resumed], [before, 0, before],
      `${pressable} of ${total} rows offer a play control the operator can press while the feed is held, of ${before} before the hold and ${resumed} after it`);
  });

  it('pausing with the panel open repaints the rows', () => {
    const screen = searchScreen();
    const before = playControls(screen.panel);

    control(screen.el, 'pause').click();

    const after = playControls(screen.panel);

    assert.notEqual(after[0], before[0], 'the rows the hold is about were never repainted: the hold reached nobody');
    assert.equal(after.length, before.length, 'the repaint lost rows the search answered with');
  });

  it('resuming the feed offers the play control again', () => {
    const screen = searchScreen();

    playingCall(screen);
    control(screen.el, 'pause').click();
    control(screen.el, 'pause').click();

    assert.equal(pressablePlayControls(screen.panel).length, playControls(screen.panel).length,
      'the rows stayed out of reach after the feed was resumed');
  });

  it('a held feed keeps the stop row as it is', () => {
    const screen = searchScreen();

    playingCall(screen, 101);

    const controls = rowControls(screen.panel);

    assert.deepEqual(controls, ['Stop', 'Play', 'Play', 'Play'], 'the rows the operator reads by what they do');

    control(screen.el, 'pause').click();

    // the hold reaches the rows that start nothing, and leaves the control the call in flight needs where it was.
    assert.deepEqual(rowControls(screen.panel), ['Stop', 'Play (out of reach)', 'Play (out of reach)', 'Play (out of reach)'],
      'the held feed changed the stop the call in flight needs, or left a play control within reach');
  });

  it('a held feed keeps the loading row as it is', () => {
    const screen = searchScreen();

    screen.useCases.loadAndPlay(102);

    const controls = rowControls(screen.panel);

    assert.deepEqual(controls, ['Play', 'Loading (out of reach)', 'Play', 'Play'], 'the row whose recording is on its way says so');

    control(screen.el, 'pause').click();

    // the hold leaves the row whose recording is on its way named and out of reach, as it was.
    assert.deepEqual(rowControls(screen.panel), ['Play (out of reach)', 'Loading (out of reach)', 'Play (out of reach)', 'Play (out of reach)'],
      'the held feed changed the row whose recording is on its way, or left a play control within reach');
  });

  it('a held feed keeps the download rows as they are', () => {
    const screen = searchScreen();

    screen.panel.downloadMode.button.click();

    const labels = findAll(screen.panel.tbody, (node) => node.tag === 'button').map(labelOf);

    assert.deepEqual(labels, ['Download', 'Download', 'Download', 'Download'], 'the rows download mode renders');

    control(screen.el, 'pause').click();

    assert.deepEqual(findAll(screen.panel.tbody, (node) => node.tag === 'button').map(labelOf), labels,
      'the held feed changed a row download mode rendered');
  });

  it('a hold leaves the rows, their order, and the filters as they were', () => {
    const screen = searchScreen();

    const rows = rowLabels(screen.panel);
    const sort = screen.panel.form.elements.sort.value;
    const count = screen.store.getState().playbackList.count;

    control(screen.el, 'pause').click();

    assert.deepEqual(rowLabels(screen.panel), rows, 'the hold moved or took a row the search answered with');
    assert.equal(screen.panel.form.elements.sort.value, sort, 'the hold changed a filter the operator set');
    assert.equal(screen.store.getState().playbackList.count, count, 'the hold cleared the answer the search gave');
  });

  it('a hold does not move the keyboard off the control that pressed it', () => {
    const screen = searchScreen();
    const pause = control(screen.el, 'pause');

    pause.focus();
    pause.click();

    assert.equal(document.activeElement, pause, 'holding the feed moved the keyboard off the control the operator pressed');

    pause.click();

    assert.equal(document.activeElement, pause, 'resuming the feed moved the keyboard off the control the operator pressed');
  });
});

// The walk a row starts, and the other ways it ends. Only the row's own stop was changed; the
// walk, the search the operator starts from the form, and download mode are read here as the
// page leaves them.
describe('presentation/listener/rdio-search-panel — the walk the rows start keeps walking', () => {
  it('the walk steps on after a stop ended the walk it was on', () => {
    const screen = searchScreen();

    playingCall(screen, 101).click();

    assert.equal(screen.useCases.livefeedMode, 'offline', 'the stop left the page in the walk it was supposed to end');

    playingCall(screen, 103);

    const before = screen.sent().length;

    control(screen.el, 'skip').click();

    assert.deepEqual(screen.requests(before), [['CAL', '104', 'play']],
      `the walk the operator started again asked for ${JSON.stringify(screen.requests(before))}`);
  });

  it('the walk steps on after the feed was held and resumed', () => {
    const screen = searchScreen();

    playingCall(screen, 101);

    control(screen.el, 'pause').click();
    control(screen.el, 'pause').click();

    const before = screen.sent().length;

    control(screen.el, 'skip').click();

    assert.deepEqual(screen.requests(before), [['CAL', '102', 'play']],
      `the walk asked for ${JSON.stringify(screen.requests(before))} after the feed was let go`);
  });

  it('a search from the filter form ends the walk the page is on', () => {
    const screen = searchScreen();

    playingCall(screen, 101);

    const before = screen.sent().length;

    pick(screen.panel, 'system', 'County');

    assert.ok(screen.sent().slice(before).some((frame) => frame[0] === 'LCL'),
      'the filter the operator changed asked the server for no new search');
    assert.equal(screen.useCases.livefeedMode, 'offline',
      `the new search left the app in ${screen.useCases.livefeedMode} mode instead of ending the walk`);
    assert.equal(screen.store.getState().call ?? null, null, 'the call the walk was playing stayed on the display');

    const asked = screen.sent().length;

    control(screen.el, 'skip').click();

    assert.deepEqual(screen.requests(asked), [], 'the walk the search ended stepped on to another searched call');
  });

  it('a download row asks for the recording and starts no walk', () => {
    const screen = searchScreen();

    screen.panel.downloadMode.button.click();

    const before = screen.sent().length;

    actionFor(screen.panel, 102).click();

    assert.deepEqual(screen.sent().slice(before), [['CAL', '102', 'download']],
      `the row download mode renders asked the server for ${JSON.stringify(screen.sent().slice(before))}`);
    assert.equal(screen.useCases.livefeedMode, 'offline',
      `a download left the app in ${screen.useCases.livefeedMode} mode`);
    assert.deepEqual(rowControls(screen.panel), ['Download', 'Download', 'Download', 'Download'],
      'a row stopped offering the control download mode renders');
  });

  it('a store change the panel does not read leaves the rows where they are', () => {
    const screen = searchScreen();

    const before = playControls(screen.panel);

    assert.ok(before.length, 'the list offers no play control to repaint');

    screen.store.setState({ queue: 3 });

    const after = playControls(screen.panel);

    assert.equal(after.length, before.length, 'a number the panel does not read took or added a row control');
    assert.equal(after[0], before[0], 'a number the panel does not read repainted the rows it shows');
  });
});
