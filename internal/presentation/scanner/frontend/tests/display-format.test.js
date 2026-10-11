// The locale hour cycle (T022, CA-010): the browser locale decides whether the
// clock is 12-hour or 24-hour; no administrator option controls it any more.

import test from 'node:test';
import assert from 'node:assert/strict';

import { formatDuration, formatTime } from '../../../common/frontend/presentation/display-format.js';

test('a 12-hour locale renders the clock with the AM/PM suffix', () => {
  const date = new Date(2026, 9, 10, 13, 5);

  assert.equal(formatTime(date, 'en-US'), '01:05 PM');
  assert.equal(formatTime(new Date(2026, 9, 10, 0, 7), 'en-US'), '12:07 AM');
});

test('a 24-hour locale renders the clock in 24-hour time', () => {
  const date = new Date(2026, 9, 10, 13, 5);

  assert.equal(formatTime(date, 'de-DE'), '13:05');
  assert.equal(formatTime(new Date(2026, 9, 10, 0, 7), 'de-DE'), '00:07');
});

test('a non-date renders an empty clock', () => {
  assert.equal(formatTime(null, 'en-US'), '');
  assert.equal(formatTime('not a date', 'en-US'), '');
});

// The search table's duration column: mm:ss from the seconds the search row
// names, blank when the length was never measured.

test('a measured length renders minutes and seconds', () => {
  assert.equal(formatDuration(12.345), '00:12');
  assert.equal(formatDuration(75), '01:15');
  assert.equal(formatDuration(3600), '60:00');
  assert.equal(formatDuration(9.5), '00:09');
});

test('a length never measured renders blank', () => {
  assert.equal(formatDuration(0), '');
  assert.equal(formatDuration(-4), '');
  assert.equal(formatDuration(undefined), '');
  assert.equal(formatDuration(null), '');
  assert.equal(formatDuration('12'), '');
});
