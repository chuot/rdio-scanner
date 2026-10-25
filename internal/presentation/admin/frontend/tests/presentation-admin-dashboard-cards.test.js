// the six dashboard cards: order, state words, reasons, the chart as a text list
// every figure text; read-only, nothing on it saves
// readings are taken on the rendered element as the operator meets it

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';
import { STATUS_REFRESH_DELAY } from '../domain/dashboard-status.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-dashboard.js';

import { findAll } from './dom-shim.js';
import { holdRefreshInterval } from './refresh-clock.js';

// the section's own refresh schedule is held so it cannot poll under the test
// the dashboard refresh test covers when the section polls
holdRefreshInterval(STATUS_REFRESH_DELAY);

const STATUS = {
  server: {
    version: '8.0.0',
    docker: false,
    startedAt: '2026-10-17T06:31:03Z',
    now: '2026-10-17T08:45:11Z',
    uptimeSeconds: 8048,
    baseDir: '/data/rdio',
    databaseBytes: 4831838208,
    databaseKnown: true,
  },
  live: { listeners: 3, administratorSessions: 1, dirwatchersRunning: 2, heldCalls: 2, audioConversion: true, audioProbe: true },
  activity: {
    windowStart: '2026-10-16T08:00:00Z',
    windowEnd: '2026-10-17T08:00:00Z',
    callsToday: 214,
    callsLastHour: 37,
    lastCallAt: '2026-10-17T08:44:02Z',
    hours: Array.from({ length: 24 }, (_, index) => ({
      start: new Date(Date.parse('2026-10-16T08:00:00Z') + index * 3600000).toISOString(),
      count: index === 21 ? 9 : index === 23 ? 37 : 0,
    })),
    busiest: [
      { systemRef: 1, talkgroupRef: 101, name: 'Dispatch', label: 'T-101', count: 41 },
      { systemRef: 1, talkgroupRef: 102, name: 'Interop', label: 'T-102', count: 12 },
    ],
    recent: [],
  },
  logs: {
    windowStart: '2026-10-16T08:45:11Z',
    errors: 3,
    warnings: 11,
    sources: [
      { source: 'dirwatch', errors: 2, warnings: 5 },
      { source: 'downstream', errors: 1, warnings: 6 },
    ],
  },
};

const CONFIG = {
  access: [],
  apikeys: [{ id: 1, key: 'capture-admin-token' }],
  dirwatch: [
    { id: 2, directory: '/var/rdio/one', disabled: false },
    { id: 3, directory: '/var/rdio/two', disabled: false },
    { id: 4, directory: '/var/rdio/three', disabled: true },
  ],
  downstreams: [
    { id: 5, url: 'https://one.example', disabled: false },
    { id: 6, url: 'https://two.example', disabled: true },
  ],
  groups: [{ id: 7, label: 'Dispatch' }],
  options: { branding: 'shed' },
  systems: [
    {
      id: 8,
      label: 'Trunk',
      sites: [{ id: 9, label: 'East' }],
      talkgroups: [{ id: 10, label: 'Dispatch' }, { id: 11, label: 'Interop' }],
      units: [{ id: 12, label: 'Car 1' }],
    },
  ],
};

// the status read is answered here so opening the section needs no server
// and answers with the seeded snapshot, so the read cannot change the display
let refused = false;

// the served snapshot equals the seeded one
// so the read on open cannot repaint the section under the test's hands
let served = STATUS;

globalThis.fetch = async () => {
  if (refused) {
    return {
      status: 417,
      ok: false,
      async json() {
        throw new Error('no body');
      },
    };
  }

  return {
    status: 200,
    ok: true,
    async json() {
      return structuredClone(served);
    },
  };
};

function openDashboard(status = STATUS) {
  const context = getAdminContext();

  served = status;

  context.store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    config: structuredClone(CONFIG),
    todos: [],
    status: { snapshot: structuredClone(status), failed: false, fetchedAt: status.server?.now ?? STATUS.server.now },
  });

  const el = new (customElements.get('rdio-admin-dashboard'))();

  el.connectedCallback();

  return el;
}

function cards(el) {
  return findAll(el, (node) => node.tag === 'section' && node.classList?.contains('rdio-dashboard-card'));
}

function card(el, key) {
  return cards(el).find((node) => node.dataset.card === key) ?? null;
}

function text(node) {
  if (!node) return '';

  if (node.tag === '#text') return node._text ?? '';

  return [node._text ?? '', ...(node.children ?? []).map(text)].filter((part) => part !== '').join(' ');
}

function stateOf(el, key) {
  const pill = findAll(card(el, key) ?? el, (node) => node.classList?.contains('rdio-dashboard-state'))[0];

  return { word: text(pill).trim(), state: pill?.dataset.state ?? null };
}

function reasonOf(el, key) {
  const reason = findAll(card(el, key) ?? el, (node) => node.classList?.contains('rdio-dashboard-reason'))[0];

  return text(reason).trim();
}

function figure(el, key, label) {
  const term = findAll(card(el, key), (node) => node.tag === 'dt' && text(node).trim() === label)[0];

  return term ? text(term.parent).trim() : null;
}

function readLineText(el) {
  return text(findAll(el, (node) => node.classList?.contains('rdio-dashboard-read'))[0]).trim();
}

// fixture stamp naming the current instant:
// freshness compares the page's clock, so printed dates hinge on test timing
function readNow() {
  const now = new Date().toISOString();

  return { ...STATUS, server: { ...STATUS.server, now } };
}

// deployment status leads the page, and the cards follow the reading order
test('the dashboard shows the six cards in the order the operator reads the deployment', () => {
  const el = openDashboard();

  const headings = findAll(el, (node) => node.tag === 'h3').map((node) => text(node).trim());

  assert.deepEqual(headings.slice(0, 6), ['Server', 'Ingestion', 'Listeners', 'Call activity', 'Busiest talkgroups', 'Log health'], `the cards are headed ${headings.join(', ')}`);

  assert.deepEqual(cards(el).map((node) => node.dataset.card), ['server', 'ingestion', 'listeners', 'activity', 'busiest', 'logHealth']);

  // each card is a region named by its heading, so it reads as a name, not figures
  for (const node of cards(el)) {
    const headingId = node.getAttribute('aria-labelledby');

    assert.ok(headingId, `the ${node.dataset.card} card is not named by its heading`);

    const heading = findAll(node, (child) => child.tag === 'h3' && child.getAttribute('id') === headingId)[0];

    assert.ok(heading, `the ${node.dataset.card} card names ${headingId}, which is not a heading inside it`);
  }

  el.disconnectedCallback();
});

// the pill's word is the domain module's word; colour carries nothing alone
test('each card answers with one state word, and the word is not carried by colour alone', () => {
  const el = openDashboard();

  for (const key of ['server', 'ingestion', 'listeners', 'activity', 'busiest']) {
    const { word, state } = stateOf(el, key);

    assert.equal(state, 'ok', `the ${key} card is not marked as working`);
    assert.equal(word, 'Working', `the ${key} card's state word is "${word}"`);
  }

  const logHealth = stateOf(el, 'logHealth');

  assert.equal(logHealth.state, 'attention');
  assert.equal(logHealth.word, 'Needs attention', `the log health card's state word is "${logHealth.word}"`);

  el.disconnectedCallback();
});

// the reason sentence replaces the colour for operators who cannot see it
// and is the same text sighted operators read
test('the card that is not working says why in the words the operator reads', () => {
  const el = openDashboard();

  assert.match(reasonOf(el, 'logHealth'), /\b3 errors\b/, `the log health reason says "${reasonOf(el, 'logHealth')}"`);

  // a working card explains nothing; explanatory noise gets skimmed past
  assert.equal(reasonOf(el, 'server'), '', 'the server card explains itself on a server that is fine');

  el.disconnectedCallback();
});

// an enabled watch that is not watching is named on the ingestion card
// the whole live block exists for this configuration-versus-server disagreement
test('a watch that is enabled and not watching is named on the ingestion card', () => {
  const status = structuredClone(STATUS);

  status.live.dirwatchersRunning = 1;

  const el = openDashboard(status);

  const { word, state } = stateOf(el, 'ingestion');

  assert.equal(state, 'attention');
  assert.equal(word, 'Needs attention');
  assert.match(reasonOf(el, 'ingestion'), /\b2\b[\s\S]*\b1\b/, `the ingestion reason says "${reasonOf(el, 'ingestion')}", wanting the 2 enabled and the 1 running`);

  el.disconnectedCallback();
});

// every figure is text an operator can read, select, and hear
test('every figure is text, in the words the domain module chose', () => {
  const el = openDashboard();

  assert.equal(figure(el, 'server', 'Uptime'), 'Uptime 2h 14m', 'the uptime is not shown in the words the dashboard states it in');
  assert.equal(figure(el, 'server', 'Version'), 'Version 8.0.0');
  assert.equal(figure(el, 'server', 'Database'), 'Database 4.5 GB');

  // an unknown database size says so; a zero would read as an empty database
  const unknownSize = structuredClone(STATUS);

  unknownSize.server.databaseKnown = false;
  unknownSize.server.databaseBytes = 0;

  const unknown = openDashboard(unknownSize);

  assert.equal(figure(unknown, 'server', 'Database'), 'Database not known', 'a database this server cannot measure is shown as a number');

  unknown.disconnectedCallback();

  assert.equal(figure(el, 'ingestion', 'Directory watchers'), 'Directory watchers 2 of 2 enabled', 'the running watches are not shown beside the enabled ones');
  assert.equal(figure(el, 'ingestion', 'Held back'), 'Held back 2', 'the calls waiting out a hold are not shown');

  assert.equal(figure(el, 'listeners', 'Listening'), 'Listening 3');

  // Nobody listening is what a quiet deployment looks like, and the card says the number without calling
  // it a problem.
  const quiet = structuredClone(STATUS);

  quiet.live.listeners = 0;

  const quietEl = openDashboard(quiet);

  assert.equal(figure(quietEl, 'listeners', 'Listening'), 'Listening 0');
  assert.equal(stateOf(quietEl, 'listeners').state, 'ok', 'no listeners is shown as a problem');

  quietEl.disconnectedCallback();

  // the 24-hour total sums the drawn bars, so figure and chart cannot disagree
  assert.equal(figure(el, 'activity', 'Last 24 hours'), 'Last 24 hours 46 calls', 'the 24-hour total does not match the bars the chart shows');
  assert.equal(figure(el, 'activity', 'Last hour'), 'Last hour 37');
  assert.equal(figure(el, 'activity', 'Today'), 'Today 214');

  // the last call is shown in local clock time, what a 24-hour chart reads against
  // which hour it lands on is the machine's timezone's business
  assert.match(figure(el, 'activity', 'Last call') ?? '', /\d{2}:\d{2}/, `the last call is shown as "${figure(el, 'activity', 'Last call')}"`);

  assert.equal(figure(el, 'logHealth', 'Errors'), 'Errors 3');
  assert.equal(figure(el, 'logHealth', 'Warnings'), 'Warnings 11');

  el.disconnectedCallback();
});

// the busiest talkgroups are a table with named columns: name, system, count
test('the busiest talkgroups are a table with named columns', () => {
  const el = openDashboard();

  const table = findAll(card(el, 'busiest'), (node) => node.tag === 'table')[0];

  assert.ok(table, 'the busiest talkgroups are not a table');

  const headers = findAll(table, (node) => node.tag === 'th').map((node) => text(node).trim());

  assert.deepEqual(headers, ['Talkgroup', 'System', 'Calls'], `the table's columns are ${headers.join(', ')}`);

  const rows = findAll(table, (node) => node.tag === 'tr').filter((node) => findAll(node, (c) => c.tag === 'td').length);

  assert.equal(rows.length, 2, `the table carries ${rows.length} talkgroup rows, wanting the 2 the snapshot named`);
  assert.deepEqual(findAll(rows[0], (c) => c.tag === 'td').map((c) => text(c).trim()), ['Dispatch', '1', '41']);

  el.disconnectedCallback();
});

// the chart is a list stating each hour and count; the bar is decoration
test('the 24 hours are a list whose items say the hour and the count, with the bars hidden', () => {
  const el = openDashboard();

  const list = findAll(card(el, 'activity'), (node) => node.tag === 'ul')[0];

  assert.ok(list, 'the 24 hours are not a list');

  const items = findAll(list, (node) => node.tag === 'li');

  assert.equal(items.length, 24, `the chart carries ${items.length} hours, wanting the whole window`);

  for (const item of items) {
    const words = text(item).trim();

    assert.match(words, /\d{2}:\d{2}/, `an hour item does not say which hour it is: "${words}"`);
    assert.match(words, /\b\d+ calls?\b/, `an hour item does not say how many calls it holds: "${words}"`);

    const bar = findAll(item, (node) => node.classList?.contains('rdio-dashboard-bar'))[0];

    assert.ok(bar, 'an hour item has no bar');
    assert.equal(bar.getAttribute('aria-hidden'), 'true', 'the bar is not hidden from the operator who is reading the list');
  }

  // The chart's own sentence: what the bars add up to, which is the answer an operator gets without
  // counting 24 items.
  const summary = findAll(card(el, 'activity'), (node) => node.classList?.contains('rdio-dashboard-summary'))[0];

  assert.match(text(summary), /\b46 calls\b/, `the chart's summary says "${text(summary).trim()}"`);

  el.disconnectedCallback();
});

// an empty window still draws all 24 hours, not a chart that shrank to nothing
test('an empty window still draws the whole 24-hour list', () => {
  const status = structuredClone(STATUS);

  status.activity.hours = status.activity.hours.map((hour) => ({ ...hour, count: 0 }));
  status.activity.callsToday = 0;
  status.activity.callsLastHour = 0;
  status.activity.busiest = [];
  delete status.activity.lastCallAt;

  const el = openDashboard(status);

  const items = findAll(findAll(card(el, 'activity'), (node) => node.tag === 'ul')[0], (node) => node.tag === 'li');

  assert.equal(items.length, 24);

  for (const item of items) {
    assert.match(text(item), /\b0 calls\b/, `an empty hour is not shown as empty: "${text(item).trim()}"`);
  }

  // A talkgroup table with nothing in it says so, instead of being an empty frame an operator has to
  // interpret.
  assert.match(text(card(el, 'busiest')), /no calls/i, `the busiest card says "${text(card(el, 'busiest')).trim()}"`);

  el.disconnectedCallback();
});

// naming which subsystems wrote the errors beats a bare count
test('the log health card names the parts of the server that wrote the log', () => {
  const el = openDashboard();

  const words = text(card(el, 'logHealth'));

  assert.match(words, /dirwatch/, `the log health card does not name dirwatch: "${words.trim()}"`);
  assert.match(words, /downstream/, `the log health card does not name downstream: "${words.trim()}"`);

  el.disconnectedCallback();
});

// the dashboard is a read, not a form: it saves nothing, no field-like controls
test('the dashboard saves nothing and asks for nothing', () => {
  const el = openDashboard();

  const editable = findAll(el, (node) => ['input', 'select', 'textarea'].includes(node.tag));

  assert.equal(editable.length, 0, `the dashboard carries ${editable.length} control(s) that edit something`);

  assert.equal(findAll(el, (node) => node.classList?.contains('rdio-admin-save-bar')).length, 0, 'the dashboard carries a save bar');
  assert.equal(findAll(el, (node) => node.tag === 'form').length, 0, 'the dashboard carries a form');

  el.disconnectedCallback();
});

// on a refused read the held numbers are not shown beside it
// a figure with no "as of" attached is a figure an operator will believe
test('a refused read is named in text and the numbers the page holds are not presented', async () => {
  const context = getAdminContext();

  // The server's own answer to a status read it could not complete: 417, and nothing in the body.
  refused = true;

  await context.useCases.loadStatus();

  refused = false;

  assert.equal(context.store.getState().status.failed, true, 'the page does not know the read was refused');
  assert.ok(context.store.getState().status.snapshot, 'the page threw away the numbers it was holding instead of saying they are not current');

  const el = new (customElements.get('rdio-admin-dashboard'))();

  el.connectedCallback();

  const failure = findAll(el, (node) => node.classList?.contains('rdio-dashboard-failure'))[0];

  assert.ok(failure, 'the dashboard does not say the read was refused');
  assert.match(text(failure), /did not answer|refused|failed/i, `the failure says "${text(failure).trim()}"`);

  assert.equal(cards(el).length, 0, 'the dashboard still shows the cards it is holding after the read was refused');

  assert.equal(findAll(el, (node) => node.tag === 'ul').length, 0, 'the dashboard still draws the chart after the read was refused');

  el.disconnectedCallback();
});

// the last-read line attaches a moment to every figure on the page
test('the page says when it last read the server', () => {
  const el = openDashboard();

  const read = findAll(el, (node) => node.classList?.contains('rdio-dashboard-read'))[0];

  assert.ok(read, 'the page does not say when it last read the server');
  // which clock time the read fell on is the machine's timezone's business;
  // that the line names a clock time at all is the page's
  assert.match(text(read), /read at \d{2}:\d{2}/i, `the last-read line says "${text(read).trim()}"`);

  el.disconnectedCallback();
});

// the failure panel says the held figures are no longer kept current
// the last-read line carries the other half: the moment is a stale one now
test('a refused read says the figures the page is holding are no longer being kept current', async () => {
  const context = getAdminContext();

  const el = openDashboard(readNow());

  try {
    // settle the read on open first: the refusal must come after the server refused
    await context.useCases.loadStatus();

    assert.doesNotMatch(
      readLineText(el),
      /no longer keeping it current/,
      `the page calls its own figures stale before anything has gone wrong: "${readLineText(el)}"`,
    );

    refused = true;

    await context.useCases.loadStatus();

    assert.match(readLineText(el), /read at \d{2}:\d{2}/i, `the read line stopped naming the moment: "${readLineText(el)}"`);
    assert.match(
      readLineText(el),
      /no longer keeping it current/,
      `the read line says "${readLineText(el)}" while the server is refusing reads`,
    );

    // repeated refusals look identical to the store, which may signal nothing new
    // the page knows it has not read the server and keeps saying so while they fail
    // see the dashboard-staleness-after-refusals maintainer note.
    await context.useCases.loadStatus();

    assert.match(
      readLineText(el),
      /no longer keeping it current/,
      `the read line says "${readLineText(el)}" after the third refused read in a row`,
    );
  } finally {
    refused = false;

    el.disconnectedCallback();
  }
});
