// The search screen's filters, and the list they are supposed to narrow.
//
// The operator opens the search drawer, picks a system, a talkgroup, a group, or a tag, and the list
// of calls is supposed to answer with the calls that filter names. The history is stored under
// references and the operator reads labels, so the screen's job is to say which system the label
// "Metro" stands for, and to keep saying it for the filters that are labels already.
//
// The readings are taken on the rendered shell: the request the screen sends when the operator picks
// a filter, the rows the screen lists from the answer, the state the form reports while a search is
// in flight, and the dates the date field will accept.
//
// The server is not in the room. The screen's request is answered by an archive the test owns, which
// keeps the calls the criteria select by the same rule the history is filtered by: references for the
// system and the talkgroup, labels for the group and the tag. A screen that sends a label where a
// reference is required is answered by the whole archive, which is exactly what the operator sees.

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

const CONFIG = {
  email: '',
  groups: { Dispatch: {}, Interop: {} },
  tags: { Fire: {}, Talkgroup: {} },
  systems: [
    {
      id: 1,
      label: 'Metro',
      talkgroups: [
        { id: 10, label: 'Fire dispatch', groups: ['Dispatch'], tag: 'Fire' },
        { id: 11, label: 'Tac dispatch', groups: ['Interop'], tag: 'Fire' },
      ],
    },
    {
      id: 2,
      label: 'County',
      talkgroups: [{ id: 20, label: 'Sheriff', groups: ['Interop'], tag: 'Talkgroup' }],
    },
  ],
};

// The recorded calls, in the shape a search row arrives in: the references the history is stored
// under, the labels the columns show, and the group and tag labels the criteria are written with.
const ARCHIVE = [
  {
    id: 101,
    system: 1,
    talkgroup: 10,
    groups: ['Dispatch'],
    tag: 'Fire',
    dateTime: new Date(2026, 9, 6, 9, 15).toISOString(),
    audioLength: 12,
    systemData: { label: 'Metro' },
    talkgroupData: { label: 'Fire dispatch', name: 'Fire fire ground' },
  },
  {
    id: 102,
    system: 1,
    talkgroup: 11,
    groups: ['Interop'],
    tag: 'Fire',
    dateTime: new Date(2026, 9, 6, 10, 30).toISOString(),
    audioLength: 30,
    systemData: { label: 'Metro' },
    talkgroupData: { label: 'Tac dispatch', name: 'Metro tac' },
  },
  {
    id: 103,
    system: 2,
    talkgroup: 20,
    groups: ['Interop'],
    tag: 'Talkgroup',
    dateTime: new Date(2026, 9, 6, 11, 40).toISOString(),
    audioLength: 45,
    systemData: { label: 'County' },
    talkgroupData: { label: 'Sheriff', name: 'County sheriff' },
  },
];

// The rule the history is filtered by, written here so the answer the screen gets is the answer the
// criteria select — and so a criterion the screen never sent shows up as calls it never wanted.
function matching(options) {
  return ARCHIVE.filter((row) => {
    if (options.system !== undefined && row.system !== options.system) return false;
    if (options.talkgroup !== undefined && row.talkgroup !== options.talkgroup) return false;
    if (options.group !== undefined && !row.groups.includes(options.group)) return false;
    if (options.tag !== undefined && row.tag !== options.tag) return false;

    return true;
  });
}

function endsOf(matched) {
  if (!matched.length) return { dateStart: '0001-01-01T00:00:00Z', dateStop: '0001-01-01T00:00:00Z' };

  const times = matched.map((row) => row.dateTime).sort();

  return { dateStart: times[0], dateStop: times[times.length - 1] };
}

// The server answers a search with a window of two hundred calls, and the list shows ten of them at
// a time. A window the test hands the screen stands in for the answer the server already gave, so
// the readings can be taken with a search in flight on top of an answered list.
function windowOf(n) {
  return Array.from({ length: n }, (_row, i) => ({ ...ARCHIVE[0], id: 200 + i }));
}

function answeredWindow(window, count) {
  getAppContext().store.setState({
    ...getAppContext().store.getState(),
    playbackList: { count, options: { limit: 200, offset: 0 }, results: window },
    playbackListSeq: (getAppContext().store.getState().playbackListSeq ?? 0) + 1,
  });
}

// The search screen, rendered the way the page renders it. The promo and the display's clock are
// timers the browser owns and the shim does not model, so they are handed to the test during the
// render.
//
// `answers` is the search the screen asks for: when it is on, the archive answers; when it is off,
// the request is recorded and left unanswered, which is the search in flight.
function searchScreen({ answers = true } = {}) {
  document.body.replaceChildren();

  const sent = [];

  const state = getAppContext().store.getState();

  getAppContext().store.setState({ ...state, config: CONFIG });

  getAppContext().useCases.searchCalls = (options) => {
    sent.push(options);

    if (!answers) return;

    const matched = matching(options);

    getAppContext().store.setState({
      ...getAppContext().store.getState(),
      playbackList: { count: matched.length, options, ...endsOf(matched), results: matched },
      playbackListSeq: (getAppContext().store.getState().playbackListSeq ?? 0) + 1,
    });
  };

  const el = document.createElement('rdio-app');

  const realSetTimeout = globalThis.setTimeout;
  const realSetInterval = globalThis.setInterval;

  globalThis.setTimeout = () => 0;
  globalThis.setInterval = () => 0;

  try {
    el.connectedCallback();
  } finally {
    globalThis.setTimeout = realSetTimeout;
    globalThis.setInterval = realSetInterval;
  }

  document.body.append(el);

  const panel = findAll(el, (node) => node.tag === 'rdio-search-panel')[0];

  // The browser puts the sort order on the field from the markup's own selection; the shim has no
  // selected option, and the request the operator makes is measured with the order they are shown.
  panel.form.elements.sort.value = '-1';

  panel.open();

  return { el, panel, sent };
}

function pick(panel, name, value) {
  const control = panel.form.elements[name];

  control.value = value;

  panel.form.dispatchEvent({ type: 'change', target: control, bubbles: true });
}

// The list is painted to a full page of rows, and the rows past the calls the search answered with
// are blank. The operator reads the calls, so the readings are taken on the rows that hold one.
function rows(panel) {
  return findAll(panel.tbody, (node) => node.tag === 'tr').filter((row) => column(row, 4) !== '');
}

// The columns the operator reads a row by: control, date, time, duration, system, talkgroup, name.
function column(row, at) {
  return findAll(row, (node) => node.tag === 'td')[at]?.textContent ?? '';
}

function paginator(panel) {
  return findAll(panel, (node) => node.tag === 'rdio-paginator')[0];
}

function pageControl(panel, label) {
  const control = findAll(paginator(panel), (node) => node.tag === 'button' && node.getAttribute('aria-label') === label)[0];

  assert.ok(control, `the list offers no control named ${label}`);

  return control;
}

function filterControls(panel) {
  return ['sort', 'date', 'system', 'talkgroup', 'group', 'tag'].map((name) => panel.form.elements[name]);
}

describe('the search screen — the filters the operator picks narrow the list', () => {
  it('asks for the calls of the system the chosen system label stands for', () => {
    const { panel, sent } = searchScreen();

    pick(panel, 'system', 'County');

    assert.equal(sent.at(-1).system, 2, 'the request named the system by its label, not by what the history is stored under');
    assert.equal(typeof sent.at(-1).system, 'number', 'the request named the system as text');
    assert.equal(sent.at(-1).talkgroup, undefined, 'the request named a talkgroup the operator never chose');
  });

  it('lists only the calls of the system the operator chose', () => {
    const { panel } = searchScreen();

    pick(panel, 'system', 'County');

    const seen = rows(panel).map((row) => column(row, 4));

    assert.deepEqual(seen, ['County'], 'the list kept showing calls from the systems the operator filtered out');
  });

  it('asks for the talkgroup the chosen label stands for, on the system chosen with it', () => {
    const { panel, sent } = searchScreen();

    pick(panel, 'system', 'Metro');
    pick(panel, 'talkgroup', 'Tac dispatch');

    assert.deepEqual(
      { system: sent.at(-1).system, talkgroup: sent.at(-1).talkgroup },
      { system: 1, talkgroup: 11 },
      'the request named the talkgroup by its label, not by what the history is stored under',
    );
  });

  it('lists only the calls of the talkgroup the operator chose', () => {
    const { panel } = searchScreen();

    pick(panel, 'system', 'Metro');
    pick(panel, 'talkgroup', 'Tac dispatch');

    const seen = rows(panel).map((row) => column(row, 5));

    assert.deepEqual(seen, ['Tac dispatch'], 'the list kept the other talkgroup of the system in it');
  });

  it('asks for a group and a tag by the labels the history is filtered by', () => {
    const { panel, sent } = searchScreen();

    pick(panel, 'group', 'Interop');

    assert.equal(sent.at(-1).group, 'Interop', 'the request named the group as something other than its label');

    pick(panel, 'group', '');
    pick(panel, 'tag', 'Talkgroup');

    assert.equal(sent.at(-1).tag, 'Talkgroup', 'the request named the tag as something other than its label');
  });

  it('lists only the calls of the group the operator chose', () => {
    const { panel } = searchScreen();

    pick(panel, 'group', 'Dispatch');

    const seen = rows(panel).map((row) => column(row, 5));

    assert.deepEqual(seen, ['Fire dispatch'], 'the list kept the calls outside the chosen group in it');
  });

  it('lists the whole history when the operator chose nothing', () => {
    const { panel } = searchScreen();

    assert.deepEqual(rows(panel).map((row) => column(row, 4)), ['Metro', 'Metro', 'County']);
  });

  it('asks for nothing the operator did not choose', () => {
    const { sent } = searchScreen();

    assert.deepEqual(sent.at(-1), { limit: 200, offset: 0, sort: -1 }, 'the opening request carried a filter the operator never set');
  });

  it('returns the list to its first page when a filter changes', () => {
    const { panel } = searchScreen();

    answeredWindow(windowOf(200), 500);

    pageControl(panel, 'Next page').click();

    assert.equal(paginator(panel).pageIndex, 1, 'the list did not move to the next page');

    pick(panel, 'system', 'County');

    assert.equal(paginator(panel).pageIndex, 0, 'a filter change left the list on the page it was on');
  });

  it('leaves the system filter asking for the whole history when its label names no system', () => {
    const { panel, sent } = searchScreen();

    pick(panel, 'system', 'A system the server no longer has');

    assert.equal(sent.at(-1).system, undefined, 'the request sent a label the history cannot be narrowed by');
    assert.equal(rows(panel).length, 3, 'the list emptied itself over a filter the history does not know');
  });
});
