// the search screen's filters and the list they narrow: the operator
// reads labels and the history is stored under references, so the
// screen must answer with the calls the named filter selects.

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
        // A talkgroup the configuration has and the history has no recorded calls for: the filter is
        // a filter the operator can legitimately pick, and the answer is nothing.
        { id: 12, label: 'Car dispatch', groups: ['Dispatch'], tag: 'Fire' },
        // Two talkgroups the configuration numbers in the eleven-bit form: one the auto-populate
        // named with its own reference, one an operator named.
        { id: 1457, label: '1457', groups: ['Dispatch'], tag: 'Fire', type: 'edacs' },
        { id: 1458, label: 'Dispatch 1', groups: ['Dispatch'], tag: 'Fire', type: 'edacs' },
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

// the rule the history is filtered by, written here so a criterion the
// screen never sent shows up as calls it never wanted.
function matching(options) {
  return ARCHIVE.filter((row) => {
    if (options.system !== undefined && row.system !== options.system) return false;
    if (options.talkgroup !== undefined && row.talkgroup !== options.talkgroup) return false;
    if (options.group !== undefined && !row.groups.includes(options.group)) return false;
    if (options.tag !== undefined && row.tag !== options.tag) return false;

    return true;
  });
}

// the two ends of a match, as the server names them: a search that
// matches nothing reports the Unix epoch at both ends, not an absent
// span, which is what the history's own read leaves on no match.
function endsOf(matched) {
  if (!matched.length) return { dateStart: '1970-01-01T00:00:00Z', dateStop: '1970-01-01T00:00:00Z' };

  const times = matched.map((row) => row.dateTime).sort();

  return { dateStart: times[0], dateStop: times[times.length - 1] };
}

// the server answers with a window of two hundred calls, ten to a page,
// and the test hands the screen a window standing in for an answer
// already given. the store is shared by every screen and notifies only
// on a changed slice, so every render starts from a known state.
let live = null;

function resetStore() {
  getAppContext().store.setState({
    config: CONFIG,
    playbackList: undefined,
    playbackListSeq: 0,
    call: undefined,
    livefeedMode: 'offline',
  });
}

function windowOf(n) {
  return Array.from({ length: n }, (_row, i) => ({ ...ARCHIVE[0], id: 200 + i }));
}

function answeredWindow(window, count) {
  getAppContext().store.setState({
    playbackList: { count, options: { limit: 200, offset: 0 }, results: window },
    playbackListSeq: getAppContext().store.getState().playbackListSeq + 1,
  });
}

// the search screen, rendered the way the page renders it; the promo
// and the display's clock are timers the shim does not model, so they
// are handed to the test during the render. `answers` is the search
// asked for: on, the archive answers; off, the request is left open.
function searchScreen({ answers = true } = {}) {
  document.body.replaceChildren();

  const sent = [];

  live?.panel.disconnectedCallback();
  live?.el.remove();

  resetStore();

  getAppContext().useCases.searchCalls = (options) => {
    sent.push(options);

    if (!answers) return;

    const matched = matching(options);

    getAppContext().store.setState({
      playbackList: { count: matched.length, options, ...endsOf(matched), results: matched },
      playbackListSeq: getAppContext().store.getState().playbackListSeq + 1,
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

  // the browser puts the sort order on the field from the markup's own
  // selection; the shim has none, so the request is measured with it.
  panel.form.elements.sort.value = '-1';

  panel.open();

  live = { el, panel };

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
  return [...['sort', 'date', 'system', 'talkgroup', 'group', 'tag'].map((name) => panel.form.elements[name]), resetButton(panel)];
}

function resetButton(panel) {
  const control = findAll(panel, (node) => node.tag === 'button' && node.textContent === 'Reset')[0];

  assert.ok(control, 'the filter form offers no way to clear it');

  return control;
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

describe('the search screen — a search in flight', () => {
  it('says the search is in flight while it waits for the answer', () => {
    const { panel } = searchScreen({ answers: false });

    assert.equal(panel.progressBar.getAttribute('aria-busy'), 'true', 'the progress bar says nothing about the search it is waiting on');
  });

  it('says the filter form is unavailable while the search is in flight', () => {
    const { panel } = searchScreen({ answers: false });

    for (const control of filterControls(panel)) {
      assert.equal(control.getAttribute('aria-disabled'), 'true', 'a filter control says nothing about being unavailable');
    }
  });

  it('starts no second search when the operator changes a filter mid-search', () => {
    const { panel, sent } = searchScreen({ answers: false });

    const asked = sent.length;

    pick(panel, 'system', 'County');
    pick(panel, 'talkgroup', 'Sheriff');

    assert.equal(sent.length, asked, 'a filter change during a search asked the server again');
  });

  it('starts no second search when the operator presses Reset mid-search', () => {
    const { panel, sent } = searchScreen({ answers: false });

    const asked = sent.length;

    resetButton(panel).click();

    assert.equal(sent.length, asked, 'Reset during a search asked the server again');
  });

  it('asks for nothing when the operator pages while the search is in flight', () => {
    const { panel, sent } = searchScreen({ answers: false });

    answeredWindow(windowOf(200), 500);

    pick(panel, 'system', 'County');

    const asked = sent.length;

    pageControl(panel, 'Last page').click();

    assert.equal(sent.length, asked, 'the page the operator turned to asked the server for a page of its own, over the search already in flight');
  });

  it('lists the calls the server already answered with when the operator pages mid-search', () => {
    const { panel, sent } = searchScreen({ answers: false });

    answeredWindow(windowOf(200), 500);

    pick(panel, 'system', 'County');

    const asked = sent.length;

    pageControl(panel, 'Next page').click();

    assert.equal(sent.length, asked, 'turning the page inside the answered window asked the server again');
    assert.equal(rows(panel).length, 10, 'the list stopped showing the calls the server had already answered with');
  });

  it('gives the filter form back when the answer arrives', () => {
    const { panel } = searchScreen({ answers: false });

    answeredWindow([ARCHIVE[0]], 1);

    for (const control of filterControls(panel)) {
      assert.notEqual(control.getAttribute('aria-disabled'), 'true', 'the filter form stayed unavailable after the answer');
    }

    assert.equal(panel.progressBar.getAttribute('aria-busy'), 'false', 'the progress bar still says the search is in flight');
  });

  it('lets the operator change a filter again once the answer is in', () => {
    const { panel, sent } = searchScreen({ answers: false });

    answeredWindow([ARCHIVE[0]], 1);

    pick(panel, 'system', 'County');

    assert.equal(sent.length, 2, 'the filter form never came back to the operator');
    assert.equal(sent.at(-1).system, 2);
  });
});

// The talkgroup column, the row's own text, and the type the configuration
// carries beside its label. Auto-populate labels a talkgroup it has never seen
// described with its own reference as text, and such a label is the id in
// disguise rather than a name the operator chose.

describe('the search screen — the talkgroup the list names', () => {
  function callRow(id, talkgroup, talkgroupData, systemData = { label: 'Metro' }) {
    return { ...ARCHIVE[0], id, system: 1, talkgroup, talkgroupData, systemData };
  }

  // the list painted with the calls a search answered with, and the talkgroup
  // column read off each row the way the operator reads it.
  function listed(...calls) {
    const { panel } = searchScreen();

    answeredWindow(calls, calls.length);

    return { panel, cells: rows(panel).map((row) => column(row, 5)) };
  }

  it('names an agency-fleet-subfleet talkgroup by agency, fleet and subfleet', () => {
    const { panel, cells } = listed(callRow(301, 1457, { label: '1457', type: 'edacs' }));

    assert.deepEqual(cells, ['11-061'],
      'the talkgroup column writes the decimal id an edacs talkgroup is numbered in');

    const headers = findAll(panel, (node) => node.tag === 'th').map((node) => node.textContent);
    const row = rows(panel)[0];
    const cell = findAll(row, (node) => node.tag === 'td')[5];

    assert.equal(headers[5], 'Talkgroup', 'the talkgroup column has no header the row is read against');
    assert.deepEqual(findAll(cell, (node) => node.tag === 'span').map((node) => node.textContent), ['11-061'],
      'the talkgroup cell carries something other than the form as its own text');

    for (const node of [row, cell, ...findAll(cell, (node) => node.tag === 'span')]) {
      assert.equal(node.attributes.title ?? null, null, 'the list carries the other form as a tooltip');
    }
  });

  it('reads the type the system carries when the talkgroup does not carry one', () => {
    const { cells } = listed(callRow(302, 1457, { label: '1457' }, { label: 'Metro', type: 'edacs' }));

    assert.deepEqual(cells, ['11-061'],
      'the talkgroup column ignores the type the call’s system carries');
  });

  it('names a talkgroup with the label an operator wrote', () => {
    const { cells } = listed(callRow(303, 1457, { label: 'Dispatch 1', type: 'edacs' }));

    assert.deepEqual(cells, ['Dispatch 1'],
      'the talkgroup column overrode the label the operator wrote with the form the type calls for');
  });

  it('keeps the decimal id for a talkgroup the rule does not cover', () => {
    const { cells } = listed(
      callRow(304, 1457, { label: '1457', type: 'smartnet' }),
      callRow(305, 1457, { label: '1457', type: 'p25' }, { label: 'Metro', type: 'smartnet' }),
      callRow(306, 53102, { label: '53102', type: 'edacs' }),
    );

    assert.deepEqual(cells, ['1457', '1457', '53102'],
      'the talkgroup column wrote an agency-fleet-subfleet form for a talkgroup the rule does not cover');
  });
});

// The filter controls name a talkgroup by a label and send the reference that
// label stands for: the text is what the operator reads, the reference is what
// the history is narrowed by, and the two are not the same thing.

describe('the search screen — the talkgroup the filter offers', () => {
  const optionValue = (option) => option.value ?? option.attributes.value;

  function talkgroupOptions(panel) {
    return findAll(panel.form.elements.talkgroup, (node) => node.tag === 'option')
      .map((option) => ({ text: option.textContent, value: optionValue(option) }));
  }

  function offeredOptions(label) {
    const { panel, sent } = searchScreen();

    pick(panel, 'system', label);

    return { panel, sent, options: talkgroupOptions(panel) };
  }

  it('names an agency-fleet-subfleet talkgroup in the form its type numbers it in, and asks for it by its reference', () => {
    const { panel, sent, options } = offeredOptions('Metro');

    const offered = options.find((option) => option.value === '1457');

    assert.ok(offered, 'the filter dropped a talkgroup the configuration has');
    assert.equal(offered.text, '11-061',
      'the filter names the talkgroup by the decimal id its type numbers it in');

    pick(panel, 'talkgroup', offered.value);

    assert.deepEqual(
      { system: sent.at(-1).system, talkgroup: sent.at(-1).talkgroup },
      { system: 1, talkgroup: 1457 },
      'the request no longer carries the reference the label the filter offers stands for',
    );
  });

  it('names a talkgroup an operator named with the name the operator wrote', () => {
    const { options } = offeredOptions('Metro');

    const offered = options.find((option) => option.value === 'Dispatch 1');

    assert.ok(offered, 'the filter dropped the talkgroup the operator named');
    assert.equal(offered.text, 'Dispatch 1',
      'the filter replaced the label the operator wrote with the form the type calls for');
  });

  it('names a talkgroup the rule does not cover by its label', () => {
    const { options } = offeredOptions('Metro');

    for (const label of ['Fire dispatch', 'Tac dispatch', 'Car dispatch']) {
      const offered = options.find((option) => option.value === label);

      assert.ok(offered, `the filter dropped ${label}`);
      assert.equal(offered.text, label, `the filter renamed ${label} it has no agency-fleet-subfleet rule for`);
    }
  });
});

describe('the search screen — the dates the date field accepts', () => {
  it('bounds the date field to the window the search answered with', () => {
    const { panel } = searchScreen();

    pick(panel, 'system', 'Metro');

    assert.equal(panel.form.elements.date.min, '2026-10-06T09:15', 'the date field does not know the earliest call the search found');
    assert.equal(panel.form.elements.date.max, '2026-10-06T10:30', 'the date field does not know the latest call the search found');
  });

  it('leaves the date field unbounded when the search answered with no calls', () => {
    const { panel } = searchScreen();

    pick(panel, 'system', 'Metro');
    pick(panel, 'talkgroup', 'Car dispatch');

    assert.equal(rows(panel).length, 0, 'the list kept calls on screen that the filter filtered out');
    assert.equal(panel.form.elements.date.min, '', 'the date field was bounded to a date that has no calls in it');
    assert.equal(panel.form.elements.date.max, '', 'the date field was bounded to a date that has no calls in it');
  });

  it('bounds the date field to the window the operator narrowed', () => {
    const { panel } = searchScreen();

    pick(panel, 'group', 'Interop');

    assert.equal(panel.form.elements.date.min, '2026-10-06T10:30', 'the bound did not follow the filter the operator chose');
    assert.equal(panel.form.elements.date.max, '2026-10-06T11:40', 'the bound did not follow the filter the operator chose');
  });
});
