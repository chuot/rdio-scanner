// The dashboard's auto-refresh (domain/dashboard-status.js's interval, driven by the same schedule the log
// view uses). Nothing here waits: the schedule is given a timer the test controls, so every poll is
// something the test chose to run, and the interval is read back off the timer instead of slept through.

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
    // scheduledDelay is the interval the schedule is waiting out, which is how these tests read the
    // dashboard's own interval off the schedule without waiting for it.
    scheduledDelay: () => [...scheduled.values()][0]?.ms,
    run: () => {
      const due = [...scheduled.values()];

      scheduled.clear();

      for (const entry of due) entry.fn();

      return due.length;
    },
  };
}

// The dashboard is polled, and a page that is open in three tabs of an operator's browser is three pages
// asking the same question. The interval is the one the dashboard documents, and not the log view's.
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

// The pause control is clicked, the section is reopened, the page becomes visible again: every one of
// those can plausibly start the schedule again, and two schedules polling the same page is two requests
// for every interval.
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

// The status read is several statements over a database that can hold years of recorded calls. A refresh
// that fires while a read is still in flight stacks them, and the page ends up showing an older answer
// over a newer one.
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

// A read that fails — a server that went away, a session that expired — must not quietly end an auto-
// refresh the operator asked for. The schedule is scheduled before the poll runs, so it is still there.
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

// The pause control is the operator's own decision: while it is off, nothing is asked, and turning it back
// on starts exactly one chain and not the one that was already running before the pause.
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
