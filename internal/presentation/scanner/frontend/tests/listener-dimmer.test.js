// The dimmer: the display lights when a call arrives or the
// listener presses a control, and dims again 5000 ms after that. The delay is
// the domain constant DIMMER_DELAY_MS; no administrator option controls it.
// The fake clock makes the timer observable without waiting for real time.

import test from 'node:test';
import assert from 'node:assert/strict';

import { createDimmer, DIMMER_DELAY_MS } from '../presentation/listener/dimmer.js';

function fakeClock() {
  const scheduled = [];

  return {
    setTimeoutImpl: (fn, ms) => {
      scheduled.push({ fn, ms });

      return scheduled.length - 1;
    },
    clearTimeoutImpl: (id) => {
      if (id !== null) scheduled[id] = null;
    },
    fire: (index = 0) => {
      const entry = scheduled[index];

      assert.ok(entry, 'nothing is scheduled at that slot');

      entry.fn();
    },
    scheduled,
  };
}

test('the dimmer delay is the 5000 ms domain constant', () => {
  assert.equal(DIMMER_DELAY_MS, 5000);
});

test('arming the dimmer lights the display and dims it after the delay', () => {
  const clock = fakeClock();
  const seen = [];

  const dimmer = createDimmer({ ...clock, setDimmed: (dimmed) => seen.push(dimmed) });

  dimmer.arm();

  assert.deepEqual(seen, [false], 'arming must light the display');
  assert.equal(clock.scheduled.length, 1);
  assert.equal(clock.scheduled[0].ms, DIMMER_DELAY_MS);

  clock.fire(0);

  assert.deepEqual(seen, [false, true], 'the display must dim after the delay');
});

test('re-arming keeps one timer: the display stays lit for the new delay', () => {
  const clock = fakeClock();
  const seen = [];

  const dimmer = createDimmer({ ...clock, setDimmed: (dimmed) => seen.push(dimmed) });

  dimmer.arm();
  dimmer.arm();

  assert.equal(clock.scheduled.filter(Boolean).length, 1, 're-arming must cancel the old timer');

  clock.fire(1);

  assert.deepEqual(seen, [false, false, true]);
});

test('stopping the dimmer cancels the pending dim', () => {
  const clock = fakeClock();
  const seen = [];

  const dimmer = createDimmer({ ...clock, setDimmed: (dimmed) => seen.push(dimmed) });

  dimmer.arm();
  dimmer.stop();

  assert.equal(clock.scheduled.filter(Boolean).length, 0);
  assert.deepEqual(seen, [false]);
});
