// dashboard status derivations: counts, card words, the 24-hour chart, figures
// pure: the element hands in the snapshot, config, and render instant

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

// The system-name derivation is read off the module's exports as a whole, because these tests name the
// export by the name it is to be added under and the file's other tests are not about it.
import * as dashboardStatus from '../domain/dashboard-status.js';

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

// the card order and wording are pinned as the operator is shown them
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

// counts derive from the held configuration; a second source can disagree
test('the inventory counts are derived from the configuration the page holds', () => {
  const inventory = deriveInventory(configFixture());

  assert.equal(inventory.systems, 2);
  assert.equal(inventory.groups, 2);
  assert.equal(inventory.accessCodes, 2);
  assert.equal(inventory.apikeys, 1);

  // the totals are sums across every system
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

// an empty configuration answers zeros, not absent values
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

// The Busiest talkgroups card names each row's system. The configuration the page holds names every
// system twice: the store row's own `id`, which the inventory keys its per-system rows by, and the
// `systemRef` the status payload carries on every busiest row and every call. The name belongs to the
// reference, and the two numbers are not the same number.
test('a configured system reference reads as the name that configuration gives it', () => {
  const names = dashboardStatus.deriveSystemNames(
    configFixture({
      systems: [
        { id: 1, systemRef: 10001, label: 'Harbour Dispatch' },
        { id: 2, systemRef: 42002, label: 'Airport Fire Ground' },
      ],
    })
  );

  assert.equal(names.get(10001), 'Harbour Dispatch');
  assert.equal(names.get(42002), 'Airport Fire Ground');
});

// three systems, references in no order that matches the order they are listed in: a row is named by
// the system carrying its reference, not by whichever system the configuration happens to list first
test('each configured reference resolves to its own system, and not to the one listed first', () => {
  const names = dashboardStatus.deriveSystemNames(
    configFixture({
      systems: [
        { id: 1, systemRef: 42002, label: 'Airport Fire Ground' },
        { id: 2, systemRef: 10001, label: 'Harbour Dispatch' },
        { id: 3, systemRef: 30005, label: 'Ports' },
      ],
    })
  );

  assert.equal(names.get(10001), 'Harbour Dispatch');
  assert.equal(names.get(42002), 'Airport Fire Ground');
  assert.equal(names.get(30005), 'Ports');
});

// a busiest row can carry a reference for a system the configuration no longer names: an older call, or
// a system removed after its calls were recorded. The card keeps such a row's reference, so it is never
// a row with nothing in its System cell.
test('a reference no configured system carries is named by nothing', () => {
  const names = dashboardStatus.deriveSystemNames(
    configFixture({ systems: [{ id: 1, systemRef: 10001, label: 'Harbour Dispatch' }] })
  );

  assert.equal(names.has(10001), true, 'the configuration carries this reference and the derivation did not name it');
  assert.equal(names.has(10002), false, 'the derivation named a reference no configured system carries');
  assert.equal(names.get(10002), undefined, `the derivation reads ${JSON.stringify(names.get(10002))} for a reference it does not name`);
});

// a page that has never read a configuration, a configuration with no systems, and a configuration that
// is not there at all all leave every busiest row on its own reference, which is what the card prints
// before any system is named
test('a configuration with no systems in it names no reference at all', () => {
  for (const config of [configFixture({ systems: [] }), {}, null, undefined]) {
    const names = dashboardStatus.deriveSystemNames(config);

    assert.equal(names.size, 0, `the derivation named ${names.size} reference(s) read from ${JSON.stringify(config)}`);
    assert.equal(names.get(10001), undefined, `the derivation named a reference for ${JSON.stringify(config)}`);
  }
});

// a name that is not there is not a name, and a system carrying no reference cannot name a row: the
// administration form refuses both, stored data from an older version need not
test('a system that carries no name, or no reference, names nothing', () => {
  const names = dashboardStatus.deriveSystemNames({
    systems: [
      { id: 1, systemRef: 10001, label: '' },
      { id: 2, systemRef: 42002 },
      { id: 3, label: 'Airport Fire Ground' },
    ],
  });

  assert.equal(names.size, 0, `the derivation named ${names.size} reference(s) out of systems carrying neither a name nor a reference`);
});

// each card answers one state word; colour is never the only signal
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

// warnings without errors keep log health working
test('warnings alone do not make the log health card ask for attention', () => {
  const status = statusFixture({ logs: { windowStart: '2026-10-16T08:45:11Z', errors: 0, warnings: 11, sources: [{ source: 'dirwatch', errors: 0, warnings: 11 }] } });

  const card = deriveCardStates(status, deriveInventory(configFixture())).find((entry) => entry.key === 'logHealth');

  assert.equal(card.state, 'ok');
});

// nothing ingesting and nothing watching is an ingestion problem
test('nothing configured to ingest and nothing watching is an ingestion problem', () => {
  const status = statusFixture({ live: { ...statusFixture().live, dirwatchersRunning: 0 } });

  const card = deriveCardStates(status, deriveInventory(configFixture({ dirwatch: [], apikeys: [] }))).find((entry) => entry.key === 'ingestion');

  assert.equal(card.state, 'attention');
  assert.match(card.reason, /nothing/i, `the ingestion reason says "${card.reason}"`);
});

// fewer watchers running than enabled: an ingestion problem named by both numbers
test('fewer watches running than enabled is an ingestion problem, and the two numbers are the reason', () => {
  const status = statusFixture({ live: { ...statusFixture().live, dirwatchersRunning: 1 } });

  const card = deriveCardStates(status, deriveInventory(configFixture())).find((entry) => entry.key === 'ingestion');

  assert.equal(card.state, 'attention');
  assert.match(card.reason, /\b2\b.*\b1\b/, `the ingestion reason says "${card.reason}", wanting the 2 enabled and the 1 running`);
});

// a deployment that cannot convert audio says so on the ingestion card
test('a deployment that cannot convert audio says so on the ingestion card', () => {
  const status = statusFixture({ live: { ...statusFixture().live, audioConversion: false } });

  const card = deriveCardStates(status, deriveInventory(configFixture())).find((entry) => entry.key === 'ingestion');

  assert.equal(card.state, 'attention');
  assert.match(card.reason, /convert/i, `the ingestion reason says "${card.reason}"`);
});

// zero listeners is a normal quiet state, not a problem
test('no listeners is a normal state and not a problem', () => {
  const status = statusFixture({ live: { ...statusFixture().live, listeners: 0 } });

  const card = deriveCardStates(status, deriveInventory(configFixture())).find((entry) => entry.key === 'listeners');

  assert.equal(card.state, 'ok');
});

// history stopped growing: calls exist, ingestion is set, nothing arrived in a day
// every other figure is zero and the server is healthy; only this names it
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

// a snapshot block that did not answer reads as unknown, not zero
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

// the default password: one derivation read by the server card and the todo list
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

// the chart spans the whole window whatever the history holds
test('the hour series is the whole window, oldest first, with a ratio for the bar', () => {
  const series = buildHourSeries(statusFixture());

  assert.equal(series.length, 24);

  assert.equal(series[0].count, 0);
  assert.equal(series[23].count, 37);

  // ratio is the bar height against the busiest hour, so quiet hours still draw
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

// the hour labels are the words of the chart's text alternative
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

  // an unknown size is words, never a zero an operator could misread
  assert.equal(formatBytes(0, false), 'not known');
});

// a snapshot older than three refresh intervals is not current
test('a snapshot older than three refresh intervals is not current', () => {
  const fetchedAt = '2026-10-17T08:45:11Z';

  assert.equal(isStale(fetchedAt, '2026-10-17T08:45:16Z'), false);
  assert.equal(isStale(fetchedAt, '2026-10-17T08:45:21Z'), false);

  assert.equal(isStale(fetchedAt, new Date(Date.parse(fetchedAt) + STATUS_STALE_AFTER_MS).toISOString()), true);
  assert.equal(isStale(fetchedAt, '2026-10-17T09:30:00Z'), true);

  // a page holding no snapshot is not holding a stale one
  assert.equal(isStale(null, '2026-10-17T09:30:00Z'), false);
});

test('the staleness window is three refresh intervals', () => {
  assert.equal(STATUS_STALE_AFTER_MS, 3 * STATUS_REFRESH_DELAY);
});
