// The locale hour cycle (T022, CA-010): the browser locale decides whether the
// clock is 12-hour or 24-hour; no administrator option controls it any more.

import test from 'node:test';
import assert from 'node:assert/strict';

import { formatTime } from '../../../common/frontend/presentation/display-format.js';

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
