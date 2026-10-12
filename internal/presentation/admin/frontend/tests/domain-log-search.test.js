// The administrator's log view's filters and counts: what the filters turn into a request,
// and what the server's per-subsystem counts turn into what the view shows. The element owns the rendering;
// these are the decisions it makes.

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

// The whole point of the subsystem filter is that an operator can ask about a part of the server by name.
// A filter that offers a subset of the vocabulary is a log view that cannot be narrowed to the subsystem
// the operator is troubleshooting.
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

// A filter the operator did not set is not sent. The server reads an absent criterion as no filter; an
// empty one is the view claiming a filter it does not have, and a text filter of "" would be a search for
// a substring every message contains.
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

// The view pages ten entries at a time over server pages of two hundred, and asks for the server page the
// client window falls inside — which is what makes paging backwards work instead of drifting.
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

// A server newer than this client can report a subsystem the view has no label for. Hiding it would be a
// part of the server the operator cannot narrow to, so it is shown under its own name.
test('a subsystem the view has no label for is still shown, under its own name', () => {
  assert.deepEqual(logSourceCounts([{ source: 'somethingnew', count: 3 }]), [
    { source: 'somethingnew', label: 'somethingnew', count: 3 },
  ]);
});

test('a search that matched nothing renders no counts', () => {
  assert.deepEqual(logSourceCounts([]), []);
  assert.deepEqual(logSourceCounts(undefined), []);
});
