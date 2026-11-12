// log view filters and counts: what the filters turn into a request,
// and what the server's per-subsystem counts become on the view

import test from 'node:test';
import assert from 'node:assert/strict';

import {
  LOG_CLIENT_PAGE,
  LOG_SERVER_PAGE,
  LOG_SOURCES,
  logSearchFilterOptions,
  logSearchOptions,
  logSourceCounts,
} from '../domain/log-search.js';

// the subsystem filter offers the server's whole vocabulary, not a subset
test('the subsystem filter offers every subsystem the server has', () => {
  const offered = logSearchFilterOptions();

  assert.deepEqual(offered[0], ['', 'All Subsystems']);

  assert.equal(offered.length, LOG_SOURCES.length + 1, 'the filter offers a different number of subsystems than the vocabulary');

  for (const [value, label] of LOG_SOURCES) {
    const entry = offered.find(([v]) => v === value);

    assert.ok(entry, `the filter does not offer ${value}`);
    assert.ok(label, `the subsystem ${value} has no label`);
    assert.notEqual(label, '', `the subsystem ${value} has an empty label`);
  }
});

// a maintenance run files what it removed under its own subsystem, and the operator has to be able
// to filter the log for it: the choice is offered, it reads as a subsystem, and it sits with the
// subsystems this server writes rather than after the names only a migrated database carries.
test('the log view offers the maintenance subsystem among the subsystems this server writes', () => {
  const offered = logSearchFilterOptions().slice(1);
  const values = offered.map(([value]) => value);

  const at = values.indexOf('maintenance');

  assert.notEqual(at, -1, 'the subsystem filter does not offer maintenance, so a cleared deployment cannot be looked up in the log');

  const label = offered[at][1];

  assert.ok(label.trim().length > 0, 'the maintenance subsystem is offered with an empty label');
  assert.equal(label, 'Maintenance', 'the maintenance subsystem is shown as something other than the name an operator uses for it');

  // The vocabulary lists what this server writes first, and the names only a database migrated from
  // the previous server carries last. What a run of this server removed belongs in the first group.
  const written = values.slice(0, values.indexOf('calls'));

  assert.ok(written.includes('maintenance'), `maintenance is offered at position ${at + 1}, after the subsystems no writer in this server files under: ${values}`);
  assert.ok(values.indexOf('admin') < at, 'maintenance is offered before Administration, which this server has listed for longer than it has had maintenance runs');

  assert.deepEqual(logSourceCounts([{ source: 'maintenance', count: 6 }]), [
    { source: 'maintenance', label: 'Maintenance', count: 6 },
  ]);
});

test('the filters produce the request the search is asked with', () => {
  const options = logSearchOptions({
    pageIndex: 0,
    sort: -1,
    level: 'warn',
    source: 'dirwatch',
    text: '/data/incoming',
    date: '2026-10-08T09:00:00.000Z',
  });

  assert.deepEqual(options, {
    limit: LOG_SERVER_PAGE,
    offset: 0,
    sort: -1,
    level: 'warn',
    source: 'dirwatch',
    text: '/data/incoming',
    date: '2026-10-08T09:00:00.000Z',
  });
});

// an unset filter is not sent: the server reads absent as no filter
// an empty text filter would search for a substring every message has
test('a filter the operator did not set is not sent', () => {
  assert.deepEqual(logSearchOptions({ pageIndex: 0, sort: -1, level: '', source: '', text: '', date: '' }), {
    limit: LOG_SERVER_PAGE,
    offset: 0,
    sort: -1,
  });

  assert.deepEqual(logSearchOptions({ pageIndex: 0, sort: 1, source: 'login' }), {
    limit: LOG_SERVER_PAGE,
    offset: 0,
    sort: 1,
    source: 'login',
  });
});

// ten-entry client pages ask for the server page (of 200) they fall inside
// that is what makes paging backwards work instead of drifting
test('a client page asks for the server page it falls inside', () => {
  assert.equal(logSearchOptions({ pageIndex: 0, sort: -1 }).offset, 0);
  assert.equal(logSearchOptions({ pageIndex: 19, sort: -1 }).offset, 0);
  assert.equal(logSearchOptions({ pageIndex: 20, sort: -1 }).offset, LOG_SERVER_PAGE);
  assert.equal(logSearchOptions({ pageIndex: 45, sort: -1 }).offset, 2 * LOG_SERVER_PAGE);

  assert.equal(LOG_CLIENT_PAGE, 10);
});

test('the counts are shown in the order the server reported them, most first', () => {
  const rows = logSourceCounts([
    { source: 'dirwatch', count: 137 },
    { source: 'ingest', count: 41 },
    { source: 'legacy', count: 5 },
  ]);

  assert.deepEqual(rows.map((row) => row.source), ['dirwatch', 'ingest', 'legacy']);
  assert.deepEqual(rows.map((row) => row.count), [137, 41, 5]);

  // The noisiest part of the server is the first thing an operator is offered, which is what the order is for.
  assert.equal(rows[0].label, 'Directory watchers');
});

// a subsystem with no known label is shown under its own name, never hidden
test('a subsystem the view has no label for is still shown, under its own name', () => {
  assert.deepEqual(logSourceCounts([{ source: 'somethingnew', count: 3 }]), [
    { source: 'somethingnew', label: 'somethingnew', count: 3 },
  ]);
});

test('a search that matched nothing renders no counts', () => {
  assert.deepEqual(logSourceCounts([]), []);
  assert.deepEqual(logSourceCounts(undefined), []);
});
