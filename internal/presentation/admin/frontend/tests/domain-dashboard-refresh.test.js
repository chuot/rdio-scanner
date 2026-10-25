// tests for the dashboard auto-refresh interval in domain/dashboard-status.js:
// a test-controlled timer runs every poll, and the interval is read off the timer

import test from 'node:test';
import assert from 'node:assert/strict';

import { STATUS_REFRESH_DELAY } from '../domain/dashboard-status.js';
import { createRefreshSchedule } from '../domain/log-refresh.js';

function fakeTimers() {
  let scheduled = new Map();
  let nextId = 1;

  return {
    setTimer: (fn, ms) => {
      const id = nextId++;

      scheduled.set(id, { fn, ms });

      return id;
    },
    clearTimer: (id) => {
      scheduled.delete(id);
    },
    scheduledCount: () => scheduled.size,
    // scheduledDelay reads the interval the schedule is waiting out, no waiting needed
    scheduledDelay: () => [...scheduled.values()][0]?.ms,
    run: () => {
      const due = [...scheduled.values()];

      scheduled.clear();

      for (const entry of due) entry.fn();

      return due.length;
    },
  };
}

// the dashboard polls on its own documented interval, not the log view's
test('the dashboard polls on its own interval', () => {
  const timers = fakeTimers();

  let polls = 0;

  const schedule = createRefreshSchedule({ poll: () => polls++, delay: STATUS_REFRESH_DELAY, ...timers });

  schedule.start();

  assert.equal(timers.scheduledDelay(), 5000, 'the dashboard is not polling on the interval it documents');

  timers.run();

  assert.equal(polls, 1);
  assert.equal(timers.scheduledDelay(), 5000, 'the dashboard changed its interval after the first poll');
});

// starting the schedule again (pause, reopen, visibility) must not double the polling
test('starting a schedule that is already running does not double the polling', () => {
  const timers = fakeTimers();

  let polls = 0;

  const schedule = createRefreshSchedule({ poll: () => polls++, delay: STATUS_REFRESH_DELAY, ...timers });

  schedule.start();
  schedule.start();
  schedule.start();

  assert.equal(timers.scheduledCount(), 1);

  timers.run();

  assert.equal(polls, 1);
});

// a refresh while a status read is in flight would stack reads and show an older answer
// over a newer one
test('a poll is not stacked on one already in flight, and the schedule carries on', () => {
  const timers = fakeTimers();

  let polls = 0;
  let busy = false;

  const schedule = createRefreshSchedule({ poll: () => polls++, isBusy: () => busy, delay: STATUS_REFRESH_DELAY, ...timers });

  schedule.start();

  busy = true;

  timers.run();

  assert.equal(polls, 0, 'the schedule polled while a read was in flight');
  assert.equal(timers.scheduledCount(), 1, 'a skipped poll ended the schedule');

  busy = false;

  timers.run();

  assert.equal(polls, 1);
});

// the schedule is armed before the poll runs, so a failed read cannot end the refresh
test('a poll that fails does not end the refresh', () => {
  const timers = fakeTimers();

  let polls = 0;

  const schedule = createRefreshSchedule({
    poll: () => {
      polls++;

      if (polls === 1) throw new Error('the status read was refused');
    },
    delay: STATUS_REFRESH_DELAY,
    ...timers,
  });

  schedule.start();

  assert.throws(() => timers.run(), /status read was refused/);

  assert.equal(polls, 1);
  assert.equal(timers.scheduledCount(), 1, 'a failed poll ended the schedule');
  assert.equal(schedule.isRunning(), true);

  timers.run();

  assert.equal(polls, 2);
});

// pause asks for nothing; resume starts exactly one chain, not the paused one
test('pausing ends the polling and resuming starts exactly one', () => {
  const timers = fakeTimers();

  let polls = 0;

  const schedule = createRefreshSchedule({ poll: () => polls++, delay: STATUS_REFRESH_DELAY, ...timers });

  schedule.start();

  timers.run();

  assert.equal(polls, 1);

  schedule.stop();

  assert.equal(timers.scheduledCount(), 0, 'the schedule is still waiting after it was paused');
  assert.equal(schedule.isRunning(), false);

  assert.equal(timers.run(), 0);
  assert.equal(polls, 1, 'a paused dashboard polled anyway');

  schedule.start();

  assert.equal(timers.scheduledCount(), 1);

  schedule.start();

  assert.equal(timers.scheduledCount(), 1, 'resuming twice started two chains');

  timers.run();

  assert.equal(polls, 2);
});
