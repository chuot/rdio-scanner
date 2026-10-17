// The dashboard's own derivations (domain/dashboard-status.js): the counts the page reads out of the
// configuration it already holds, the one word each card answers with and the sentence that says why, the
// 24 hours the chart is drawn over, and the words the figures are shown in. Nothing here touches the DOM,
// the network, or the clock: the element hands in the snapshot, the configuration, and the instant it is
// rendering at.

import assert from 'node:assert/strict';
import test from 'node:test';

import {
  DASHBOARD_CARDS,
  STATUS_REFRESH_DELAY,
  STATUS_STALE_AFTER_MS,
  buildHourSeries,
  deriveCardStates,
  deriveInventory,
  formatBytes,
  formatUptime,
  isStale,
  summarizeHourSeries,
} from '../domain/dashboard-status.js';

// The snapshot the server answers, in the shape the published contract documents.
function statusFixture(overrides = {}) {
  const base = {
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
    live: {
      listeners: 3,
      administratorSessions: 1,
      dirwatchersRunning: 2,
      heldCalls: 2,
      audioConversion: true,
      audioProbe: true,
    },
    activity: {
      windowStart: '2026-10-16T08:00:00Z',
      windowEnd: '2026-10-17T08:00:00Z',
      callsToday: 214,
      callsLastHour: 37,
      lastCallAt: '2026-10-17T08:44:02Z',
      hours: hourBuckets([0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 9, 4, 37]),
      busiest: [{ systemRef: 1, talkgroupRef: 101, name: 'Dispatch', label: 'T-101', count: 41 }],
      recent: [
        { callId: 91827, systemRef: 1, talkgroupRef: 101, timestamp: '2026-10-17T08:44:02Z', audioLengthSeconds: 12.4 },
        { callId: 91826, systemRef: 1, talkgroupRef: 102, timestamp: '2026-10-17T08:31:40Z', audioLengthSeconds: 0 },
      ],
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

  return { ...base, ...overrides };
}

// hourBuckets is the 24 whole hours the window is cut into, oldest first, starting where the fixture's
// window starts.
function hourBuckets(counts) {
  return counts.map((count, index) => ({
    // The instant is spelled the way the server spells it: whole seconds, no milliseconds.
    start: new Date(Date.parse('2026-10-16T08:00:00Z') + index * 3600000).toISOString().replace(/\.\d{3}Z$/, 'Z'),
    count,
  }));
}

function configFixture(overrides = {}) {
  const base = {
    access: [
      { id: 1, user: 'radio', code: 'AAAA' },
      { id: 2, user: 'radio', code: 'BBBB' },
    ],
    apikeys: [{ id: 3, key: 'capture-admin-token' }],
    dirwatch: [
      { id: 1, directory: '/var/rdio/one', disabled: false },
      { id: 2, directory: '/var/rdio/two', disabled: false },
      { id: 3, directory: '/var/rdio/three', disabled: true },
    ],
    downstreams: [
      { id: 4, url: 'https://one.example', disabled: false },
      { id: 5, url: 'https://two.example', disabled: true },
    ],
    groups: [{ id: 6, label: 'Dispatch' }, { id: 7, label: 'Talkgroups' }],
    systems: [
      {
        id: 8,
        label: 'Trunk',
        sites: [{ id: 9, label: 'East' }, { id: 10, label: 'West' }],
        talkgroups: [{ id: 11, label: 'Dispatch' }, { id: 12, label: 'Interop' }, { id: 13, label: 'Simple' }],
        units: [{ id: 14, label: 'Car 1' }],
      },
      { id: 15, label: 'Mastodon', sites: [], talkgroups: [], units: [] },
    ],
    options: { branding: 'shed' },
  };

  return { ...base, ...overrides };
}

// The card list is the order the operator reads the page in, and the words are the words the navigation
// and the headings carry. Written out here because they are what an operator is shown.
test('the dashboard shows six cards, in the order the operator reads them', () => {
  assert.deepEqual(
    DASHBOARD_CARDS.map((card) => card.key),
    ['server', 'ingestion', 'listeners', 'activity', 'busiest', 'logHealth'],
  );

  assert.deepEqual(
    DASHBOARD_CARDS.map((card) => card.label),
    ['Server', 'Ingestion', 'Listeners', 'Call activity', 'Busiest talkgroups', 'Log health'],
  );
});

test('the refresh interval is the one the dashboard documents', () => {
  assert.equal(STATUS_REFRESH_DELAY, 5000);
});

// The inventory is derived from the configuration the page already holds: the status read carries no
// inventory, and a second source for a count the page can compute itself is a count that can disagree
// with the page.
test('the inventory counts are derived from the configuration the page holds', () => {
  const inventory = deriveInventory(configFixture());

  assert.equal(inventory.systems, 2);
  assert.equal(inventory.groups, 2);
  assert.equal(inventory.accessCodes, 2);
  assert.equal(inventory.apikeys, 1);

  // The talkgroup, site, and unit totals are the sums across every system, which is the number the
  // operator is asking for when they ask how many talkgroups this deployment has.
  assert.equal(inventory.talkgroups, 3);
  assert.equal(inventory.sites, 2);
  assert.equal(inventory.units, 1);
});

test("the inventory names each system's own totals, so a total is not mistaken for one system", () => {
  const perSystem = deriveInventory(configFixture()).perSystem;

  assert.deepEqual(perSystem, [
    { id: 8, label: 'Trunk', talkgroups: 3, sites: 2, units: 1 },
    { id: 15, label: 'Mastodon', talkgroups: 0, sites: 0, units: 0 },
  ]);
});

test('the inventory counts the watches and downstreams that are configured and the ones disabled', () => {
  const inventory = deriveInventory(configFixture());

  assert.deepEqual(inventory.dirwatch, { configured: 3, enabled: 2, disabled: 1 });
  assert.deepEqual(inventory.downstreams, { configured: 2, enabled: 1, disabled: 1 });
});

// A configuration that has nothing in it is what a fresh deployment looks like, and every count is a zero
// rather than something the page has to guard against.
test('a configuration with nothing in it answers zeros and not nothing', () => {
  const inventory = deriveInventory({});

  assert.equal(inventory.systems, 0);
  assert.equal(inventory.talkgroups, 0);
  assert.equal(inventory.sites, 0);
  assert.equal(inventory.units, 0);
  assert.deepEqual(inventory.perSystem, []);
  assert.deepEqual(inventory.dirwatch, { configured: 0, enabled: 0, disabled: 0 });
  assert.deepEqual(inventory.downstreams, { configured: 0, enabled: 0, disabled: 0 });
});

// Every card answers one word, and a card that is not working says why in a sentence the operator reads.
// Colour is never the only signal.
test('a server that is answering and a deployment that is ingesting both read as working', () => {
  const states = deriveCardStates(statusFixture(), deriveInventory(configFixture()));

  assert.deepEqual(states.map((card) => card.key), DASHBOARD_CARDS.map((card) => card.key));

  for (const key of ['server', 'ingestion', 'listeners', 'activity', 'busiest']) {
    const card = states.find((entry) => entry.key === key);

    assert.equal(card.state, 'ok', `the ${key} card reads ${card.state} on a deployment that is working`);
  }
});

// The log health card is the one card that is not working on this fixture, and it names what it found.
test('the log health card names the errors it is reporting', () => {
  const card = deriveCardStates(statusFixture(), deriveInventory(configFixture())).find((entry) => entry.key === 'logHealth');

  assert.equal(card.state, 'attention');
  assert.match(card.reason, /\b3 errors\b/, `the log health reason says "${card.reason}"`);
});

// A window with warnings and no errors is not a server in trouble: the operator would learn to ignore a
// card that cried at every warning.
test('warnings alone do not make the log health card ask for attention', () => {
  const status = statusFixture({ logs: { windowStart: '2026-10-16T08:45:11Z', errors: 0, warnings: 11, sources: [{ source: 'dirwatch', errors: 0, warnings: 11 }] } });

  const card = deriveCardStates(status, deriveInventory(configFixture())).find((entry) => entry.key === 'logHealth');

  assert.equal(card.state, 'ok');
});

// A deployment with nothing configured to ingest and nothing watching is the case the operator has to be
// told about: the server is running and nothing is arriving, and neither fact says so on its own.
test('nothing configured to ingest and nothing watching is an ingestion problem', () => {
  const status = statusFixture({ live: { ...statusFixture().live, dirwatchersRunning: 0 } });

  const card = deriveCardStates(status, deriveInventory(configFixture({ dirwatch: [], apikeys: [] }))).find((entry) => entry.key === 'ingestion');

  assert.equal(card.state, 'attention');
  assert.match(card.reason, /nothing/i, `the ingestion reason says "${card.reason}"`);
});

// The point of reporting the running watchers beside the enabled ones: a watch that is configured and
// enabled and is not watching is a broken watch, and the two numbers are what show it.
test('fewer watches running than enabled is an ingestion problem, and the two numbers are the reason', () => {
  const status = statusFixture({ live: { ...statusFixture().live, dirwatchersRunning: 1 } });

  const card = deriveCardStates(status, deriveInventory(configFixture())).find((entry) => entry.key === 'ingestion');

  assert.equal(card.state, 'attention');
  assert.match(card.reason, /\b2\b.*\b1\b/, `the ingestion reason says "${card.reason}", wanting the 2 enabled and the 1 running`);
});

// No audio converter means every recording is stored as it arrived, which is a decision the operator has
// to know about rather than discover when a player cannot play it.
test('a deployment that cannot convert audio says so on the ingestion card', () => {
  const status = statusFixture({ live: { ...statusFixture().live, audioConversion: false } });

  const card = deriveCardStates(status, deriveInventory(configFixture())).find((entry) => entry.key === 'ingestion');

  assert.equal(card.state, 'attention');
  assert.match(card.reason, /convert/i, `the ingestion reason says "${card.reason}"`);
});

// Nobody listening is what a quiet deployment looks like. A card that called it a problem would teach the
// operator to ignore the card.
test('no listeners is a normal state and not a problem', () => {
  const status = statusFixture({ live: { ...statusFixture().live, listeners: 0 } });

  const card = deriveCardStates(status, deriveInventory(configFixture())).find((entry) => entry.key === 'listeners');

  assert.equal(card.state, 'ok');
});

// The "the history stopped growing" case: the deployment has calls in it, ingestion is configured, and
// nothing has arrived for a day. Every number on the card is a zero and the server is healthy, so only
// this comparison names it.
test('a history that stopped growing in the last 24 hours asks for attention', () => {
  const status = statusFixture({
    activity: {
      ...statusFixture().activity,
      callsToday: 0,
      callsLastHour: 0,
      lastCallAt: '2026-10-10T08:44:02Z',
      hours: hourBuckets(new Array(24).fill(0)),
      busiest: [],
    },
  });

  const card = deriveCardStates(status, deriveInventory(configFixture())).find((entry) => entry.key === 'activity');

  assert.equal(card.state, 'attention');
  assert.match(card.reason, /24 hours/, `the activity reason says "${card.reason}"`);
});

// A deployment with no ingestion configured and no history has nothing to grow, so there is nothing here
// to ask about.
test('a deployment with no history and nothing configured to ingest is not asked about', () => {
  const status = statusFixture({
    activity: {
      ...statusFixture().activity,
      callsToday: 0,
      callsLastHour: 0,
      lastCallAt: undefined,
      hours: hourBuckets(new Array(24).fill(0)),
      busiest: [],
    },
  });

  const card = deriveCardStates(status, deriveInventory(configFixture({ dirwatch: [], apikeys: [] }))).find((entry) => entry.key === 'activity');

  assert.equal(card.state, 'ok');
});

// A snapshot that did not answer a block is not a snapshot that answered zero. The card says it does not
// know, which is the only honest thing it can say.
test('a snapshot missing a block answers unknown for the cards that read it', () => {
  const status = statusFixture();

  delete status.live;
  delete status.activity;
  delete status.logs;

  const states = deriveCardStates(status, deriveInventory(configFixture()));

  for (const key of ['ingestion', 'listeners', 'activity', 'busiest', 'logHealth']) {
    const card = states.find((entry) => entry.key === key);

    assert.equal(card.state, 'unknown', `the ${key} card reads ${card.state} on a snapshot that never answered it`);
    assert.ok(card.reason?.length, `the ${key} card says unknown without saying why`);
  }

  assert.equal(states.find((entry) => entry.key === 'server').state, 'ok');
});

// The default password is the server card's one problem, and it is the same fact the todo list already
// states: one derivation, read by both.
test("the default password is the server card's problem, and it says what to do", () => {
  const states = deriveCardStates(statusFixture(), deriveInventory(configFixture()), { passwordNeedChange: true });

  const card = states.find((entry) => entry.key === 'server');

  assert.equal(card.state, 'attention');
  assert.match(card.reason, /password/i, `the server reason says "${card.reason}"`);
});

test('the busiest talkgroups card never asks for attention', () => {
  const status = statusFixture({ activity: { ...statusFixture().activity, busiest: [] } });

  const card = deriveCardStates(status, deriveInventory(configFixture())).find((entry) => entry.key === 'busiest');

  assert.equal(card.state, 'ok');
});

// The chart is drawn over the whole window whatever the history holds, so a quiet server's chart has the
// same shape as a busy one's.
test('the hour series is the whole window, oldest first, with a ratio for the bar', () => {
  const series = buildHourSeries(statusFixture());

  assert.equal(series.length, 24);

  assert.equal(series[0].count, 0);
  assert.equal(series[23].count, 37);

  // The ratio is the bar's height against the busiest hour, which is what lets a quiet hour still be
  // drawn as something and not as nothing.
  assert.equal(series[23].ratio, 1);
  assert.equal(series[21].ratio, 9 / 37);

  // An hour with nothing in it still has a bar floor, so it is visible as an hour.
  assert.equal(series[0].ratio, 0);
});

test('an empty window gives every hour a zero bar and no division by nothing', () => {
  const status = statusFixture({ activity: { ...statusFixture().activity, hours: hourBuckets(new Array(24).fill(0)) } });

  for (const hour of buildHourSeries(status)) {
    assert.equal(hour.count, 0);
    assert.equal(hour.ratio, 0);
  }
});

// The hour labels are the words the chart's text alternative carries: the operator who cannot see the bars
// is told which hour is which.
test("each hour carries the label the chart's text alternative reads", () => {
  const series = buildHourSeries(statusFixture());

  for (const hour of series) {
    assert.match(hour.label, /^\d{2}:\d{2}$/, `the hour label is "${hour.label}"`);
  }

  assert.equal(series[0].hour, '2026-10-16T08:00:00Z');
});

// The chart's own sentence: what the 24 bars add up to, and which hour is the tallest one.
test("the chart's summary says the total and the busiest hour", () => {
  const summary = summarizeHourSeries(buildHourSeries(statusFixture()));

  assert.match(summary, /\b50 calls\b/, `the summary says "${summary}"`);
  assert.match(summary, /\b37\b/, `the summary says "${summary}", wanting the busiest hour's 37`);
});

test("the chart's summary for an empty window says nothing arrived", () => {
  const status = statusFixture({ activity: { ...statusFixture().activity, hours: hourBuckets(new Array(24).fill(0)) } });

  const summary = summarizeHourSeries(buildHourSeries(status));

  assert.match(summary, /no calls/i, `the summary says "${summary}"`);
});

test('the uptime is said in the words the operator reads', () => {
  assert.equal(formatUptime(41), '41s');
  assert.equal(formatUptime(2469), '41m 09s');
  assert.equal(formatUptime(8048), '2h 14m');
  assert.equal(formatUptime(90000), '1d 1h');
  assert.equal(formatUptime(273600), '3d 4h');
  assert.equal(formatUptime(0), '0s');
  assert.equal(formatUptime(undefined), '');
  assert.equal(formatUptime(-4), '');
});

test('the database size is said as a size, and a deployment that cannot know it says so', () => {
  assert.equal(formatBytes(4831838208, true), '4.5 GB');
  assert.equal(formatBytes(1048576, true), '1 MB');
  assert.equal(formatBytes(2048, true), '2 KB');
  assert.equal(formatBytes(512, true), '512 B');
  assert.equal(formatBytes(0, true), '0 B');

  // The not-known answer is words and not a zero: a zero reads as an empty database to an operator who
  // was never given a size at all.
  assert.equal(formatBytes(0, false), 'not known');
});

// A snapshot the page is holding is only current for as long as the page's own refresh interval can keep
// it current. Past that, the numbers are the numbers of a moment the page stopped reading.
test('a snapshot older than three refresh intervals is not current', () => {
  const fetchedAt = '2026-10-17T08:45:11Z';

  assert.equal(isStale(fetchedAt, '2026-10-17T08:45:16Z'), false);
  assert.equal(isStale(fetchedAt, '2026-10-17T08:45:21Z'), false);

  assert.equal(isStale(fetchedAt, new Date(Date.parse(fetchedAt) + STATUS_STALE_AFTER_MS).toISOString()), true);
  assert.equal(isStale(fetchedAt, '2026-10-17T09:30:00Z'), true);

  // A page that has never read anything is not holding a stale snapshot: it is holding nothing.
  assert.equal(isStale(null, '2026-10-17T09:30:00Z'), false);
});

test('the staleness window is three refresh intervals', () => {
  assert.equal(STATUS_STALE_AFTER_MS, 3 * STATUS_REFRESH_DELAY);
});
