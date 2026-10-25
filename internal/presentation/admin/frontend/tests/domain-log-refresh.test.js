// the log view's auto-refresh: what the toggle decides
// the schedule takes a test-controlled timer; no test ever waits

import test from 'node:test';
import assert from 'node:assert/strict';

import { LOG_REFRESH_DELAY, createRefreshSchedule } from '../domain/log-refresh.js';

function fakeTimers() {
  let scheduled = new Map();
  let nextId = 1;

  const api = {
    setTimer: (fn, ms) => {
      const id = nextId++;
      scheduled.set(id, { fn, ms });

      return id;
    },
    clearTimer: (id) => {
      scheduled.delete(id);
    },
    // scheduledCount is what is waiting right now, which is how these tests see a stopped schedule.
    scheduledCount: () => scheduled.size,
    // run fires everything waiting and reports the count, so tests step by interval
    run: () => {
      const due = [...scheduled.values()];

      scheduled.clear();

      for (const entry of due) entry.fn();

      return due.length;
    },
  };

  return api;
}

test('auto-refresh polls on the interval it was given', () => {
  const timers = fakeTimers();

  let polls = 0;

  const schedule = createRefreshSchedule({ poll: () => polls++, ...timers });

  assert.equal(polls, 0, 'turning auto-refresh on does not poll immediately: the operator is looking at what was just loaded');

  schedule.start();

  assert.equal(timers.scheduledCount(), 1);

  assert.equal(timers.run(), 1);
  assert.equal(polls, 1);

  // And it keeps going: one poll per interval, until it is turned off.
  assert.equal(timers.scheduledCount(), 1);

  timers.run();
  timers.run();

  assert.equal(polls, 3);
});

test('turning auto-refresh off stops the polling', () => {
  const timers = fakeTimers();

  let polls = 0;

  const schedule = createRefreshSchedule({ poll: () => polls++, ...timers });

  schedule.start();

  timers.run();

  assert.equal(polls, 1);

  schedule.stop();

  assert.equal(timers.scheduledCount(), 0, 'the schedule is still waiting after it was stopped');

  assert.equal(timers.run(), 0);
  assert.equal(polls, 1, 'a stopped schedule polled anyway');

  assert.equal(schedule.isRunning(), false);
});

// starting a running schedule twice would double the polling
test('starting a schedule that is already running does not double the polling', () => {
  const timers = fakeTimers();

  let polls = 0;

  const schedule = createRefreshSchedule({ poll: () => polls++, ...timers });

  schedule.start();
  schedule.start();
  schedule.start();

  assert.equal(timers.scheduledCount(), 1);

  timers.run();

  assert.equal(polls, 1);
});

// a refresh during an in-flight request is skipped, and the schedule carries on
// stacked requests could render an older answer over a newer one
test('a refresh while a request is in flight is skipped, and the schedule carries on', () => {
  const timers = fakeTimers();

  let polls = 0;
  let busy = false;

  const schedule = createRefreshSchedule({ poll: () => polls++, isBusy: () => busy, ...timers });

  schedule.start();

  busy = true;

  timers.run();

  assert.equal(polls, 0, 'the schedule polled while a request was in flight');
  assert.equal(timers.scheduledCount(), 1, 'a skipped poll ended the schedule');

  busy = false;

  timers.run();

  assert.equal(polls, 1);
});

// scheduling before the poll runs keeps auto-refresh alive through a failed poll
// the failure surfaces like any failed callback; this pins the schedule survives
test('a poll that fails does not end the refresh', () => {
  const timers = fakeTimers();

  let polls = 0;

  const schedule = createRefreshSchedule({
    poll: () => {
      polls++;

      if (polls === 1) throw new Error('the log view could not be read');
    },
    ...timers,
  });

  schedule.start();

  assert.throws(() => timers.run(), /could not be read/);

  assert.equal(polls, 1);
  assert.equal(timers.scheduledCount(), 1, 'a failed poll ended the schedule');
  assert.equal(schedule.isRunning(), true);

  timers.run();

  assert.equal(polls, 2);
});

test('the refresh interval is the one the view documents', () => {
  assert.equal(LOG_REFRESH_DELAY, 10000);
});
