// The dashboard section (presentation/admin/rdio-admin-dashboard.js): the six cards, in the order the
// operator reads the deployment, each with the one word it answers with and the sentence that says why, the
// 24 hours drawn as a list an operator can read without seeing the bars, and every figure as text. The
// section is read-only like the log view: nothing on it saves anything.
//
// The readings are taken on the rendered element, the way an operator meets it: the words on the page, the
// roles the page declares, and what the page leaves out.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';
import { STATUS_REFRESH_DELAY } from '../domain/dashboard-status.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-dashboard.js';

import { findAll } from './dom-shim.js';
import { holdRefreshInterval } from './refresh-clock.js';

// The section starts its own refresh schedule when it opens, and a refresh schedule re-arms itself. Held,
// it cannot keep this process alive and cannot poll under a test that is reading what the section shows;
// when the section polls is proven in presentation-admin-dashboard-refresh.test.js.
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

// The section, opened the way the shell opens it, on the snapshot and configuration the page already
// holds. The status read is answered here so that opening the section does not reach for a server this
// test is not running, and the answer is the same snapshot the page was seeded with, so the read the
// section makes when it opens does not change what the section is showing.
let refused = false;

// The snapshot the transport answers with is the one the test just seeded, so the read the section makes
// when it opens writes back exactly what the section is already holding and cannot repaint it under the
// test's hands.
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

// The whole point of the section is that the state of the deployment is the first thing on the page, and
// the operator reads the cards in that order.
test('the dashboard shows the six cards in the order the operator reads the deployment', () => {
  const el = openDashboard();

  const headings = findAll(el, (node) => node.tag === 'h3').map((node) => text(node).trim());

  assert.deepEqual(headings.slice(0, 6), ['Server', 'Ingestion', 'Listeners', 'Call activity', 'Busiest talkgroups', 'Log health'], `the cards are headed ${headings.join(', ')}`);

  assert.deepEqual(cards(el).map((node) => node.dataset.card), ['server', 'ingestion', 'listeners', 'activity', 'busiest', 'logHealth']);

  // Each card is a region named by its own heading, so a screen reader reading the card says the card's
  // name and not a wall of figures.
  for (const node of cards(el)) {
    const headingId = node.getAttribute('aria-labelledby');

    assert.ok(headingId, `the ${node.dataset.card} card is not named by its heading`);

    const heading = findAll(node, (child) => child.tag === 'h3' && child.getAttribute('id') === headingId)[0];

    assert.ok(heading, `the ${node.dataset.card} card names ${headingId}, which is not a heading inside it`);
  }

  el.disconnectedCallback();
});

// The pill is one word, and the word is the same one the domain module decided on: a card that reads
// "working" in colour and "needs attention" in words is a card that says two things.
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

// The sentence under the pill is what the operator who cannot see the colour is told instead of the
// colour, and it is the same sentence the sighted operator reads.
test('the card that is not working says why in the words the operator reads', () => {
  const el = openDashboard();

  assert.match(reasonOf(el, 'logHealth'), /\b3 errors\b/, `the log health reason says "${reasonOf(el, 'logHealth')}"`);

  // A card that is working has nothing to explain, and a card that explains itself when nothing is wrong
  // is a card the operator learns to skim past.
  assert.equal(reasonOf(el, 'server'), '', 'the server card explains itself on a server that is fine');

  el.disconnectedCallback();
});

// A deployment with a watch configured and enabled that is not watching is the case the whole live block
// exists for: the configuration says one thing and the server says another, and the card is where the
// operator finds out.
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

// Every figure is text the operator can read, select, and hear: a dashboard that showed its numbers as
// gauges or colours would be a dashboard that only describes itself to someone looking at it.
test('every figure is text, in the words the domain module chose', () => {
  const el = openDashboard();

  assert.equal(figure(el, 'server', 'Uptime'), 'Uptime 2h 14m', 'the uptime is not shown in the words the dashboard states it in');
  assert.equal(figure(el, 'server', 'Version'), 'Version 8.0.0');
  assert.equal(figure(el, 'server', 'Database'), 'Database 4.5 GB');

  // A deployment that cannot know the size of its own database says that, and not a zero: a zero reads as
  // an empty database to an operator who was never given a size.
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

  // The 24-hour total is the sum of the bars the page is showing, so the figure and the chart cannot
  // disagree with each other.
  assert.equal(figure(el, 'activity', 'Last 24 hours'), 'Last 24 hours 46 calls', 'the 24-hour total does not match the bars the chart shows');
  assert.equal(figure(el, 'activity', 'Last hour'), 'Last hour 37');
  assert.equal(figure(el, 'activity', 'Today'), 'Today 214');

  // The instant is shown as the clock time the operator's own clock says it is, which is the clock a chart
  // of the last 24 hours is read against. Which hour it falls on is the machine's timezone's business.
  assert.match(figure(el, 'activity', 'Last call') ?? '', /\d{2}:\d{2}/, `the last call is shown as "${figure(el, 'activity', 'Last call')}"`);

  assert.equal(figure(el, 'logHealth', 'Errors'), 'Errors 3');
  assert.equal(figure(el, 'logHealth', 'Warnings'), 'Warnings 11');

  el.disconnectedCallback();
});

// The busiest talkgroups are a table: the name, the system it belongs to, and the count are three things
// an operator reads across, and a screen reader reads them as a row with named columns.
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

// The chart is a list, and every item says which hour it is and how many calls it holds. The bar is the
// shape a sighted operator reads at a glance; it carries nothing the item does not already say.
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

// A window with nothing in it is a window the chart still draws: the operator has to be able to see that
// every hour is empty, and not a chart that quietly shrank to nothing.
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

// The log health card names the parts of the server that wrote the errors, because "3 errors" is a number
// and "dirwatch wrote 2 of them" is a place to start looking.
test('the log health card names the parts of the server that wrote the log', () => {
  const el = openDashboard();

  const words = text(card(el, 'logHealth'));

  assert.match(words, /dirwatch/, `the log health card does not name dirwatch: "${words.trim()}"`);
  assert.match(words, /downstream/, `the log health card does not name downstream: "${words.trim()}"`);

  el.disconnectedCallback();
});

// The dashboard is a read of the server, not a form. Nothing on it saves anything, and a control that
// looked like a field would be a control an operator might think they have to fill in.
test('the dashboard saves nothing and asks for nothing', () => {
  const el = openDashboard();

  const editable = findAll(el, (node) => ['input', 'select', 'textarea'].includes(node.tag));

  assert.equal(editable.length, 0, `the dashboard carries ${editable.length} control(s) that edit something`);

  assert.equal(findAll(el, (node) => node.classList?.contains('rdio-admin-save-bar')).length, 0, 'the dashboard carries a save bar');
  assert.equal(findAll(el, (node) => node.tag === 'form').length, 0, 'the dashboard carries a form');

  el.disconnectedCallback();
});

// A read that was refused is said as a refusal. The numbers the page is still holding are not shown beside
// it, because a figure with no "as of" attached to it is a figure the operator will believe.
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

// The line that says when the page last read the server is what makes every figure on the page a figure
// with a moment attached to it.
test('the page says when it last read the server', () => {
  const el = openDashboard();

  const read = findAll(el, (node) => node.classList?.contains('rdio-dashboard-read'))[0];

  assert.ok(read, 'the page does not say when it last read the server');
  // Which clock time the read fell on is the machine's timezone's business; that the line names a clock time
  // at all is the page's.
  assert.match(text(read), /read at \d{2}:\d{2}/i, `the last-read line says "${text(read).trim()}"`);

  el.disconnectedCallback();
});
