// The locale hour cycle: the browser locale decides whether the
// clock is 12-hour or 24-hour; no administrator option controls it any more.

import test from 'node:test';
import assert from 'node:assert/strict';

import { formatDuration, formatTime, toLocalInputValue } from '../../../common/frontend/presentation/display-format.js';

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

// the bounds a date field is given from the two ends of a search
// answer. a datetime-local field reads its bound only as a wall-clock
// minute: any other shape is a bound the field ignores.

test('an instant becomes the wall-clock minute the date field reads', () => {
  const local = new Date(2026, 9, 6, 23, 41, 7);

  assert.equal(toLocalInputValue(local.toISOString()), '2026-10-06T23:41');
});

test('the seconds an instant carries are not part of the bound', () => {
  const local = new Date(2026, 9, 6, 8, 0, 59, 999);

  assert.equal(toLocalInputValue(local.toISOString()), '2026-10-06T08:00');
});

test('a bound is not invented for an instant that is not there', () => {
  assert.equal(toLocalInputValue(''), '');
  assert.equal(toLocalInputValue(null), '');
  assert.equal(toLocalInputValue(undefined), '');
  assert.equal(toLocalInputValue('not an instant'), '');
});

test('a bound is not invented for the zero time an empty search answers with', () => {
  assert.equal(toLocalInputValue('0001-01-01T00:00:00Z'), '');
});
