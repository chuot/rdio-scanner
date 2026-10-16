// The progress row rides the audio clock.
//
// The elapsed reading the progress row shows is the recording's own position: the engine
// anchors it to the instant the recording entered the graph, so it restarts on every new
// recording, freezes with the audio across a pause, and reads zero when nothing is playing.
// The wall clock this replaces keyed its start on the call id — a replay of the same call
// kept the old elapsed, and a paused call rode through the pause: the row jumped a whole
// minute across a 62-second pause.
//
// The tick is the component's own callback; the test captures it at mount and turns the
// engine clock by hand, so the reading is taken without waiting on wall time.

import assert from 'node:assert/strict';
import { afterEach, beforeEach, describe, it } from 'node:test';

import './dom-shim.js';

import '../../../common/frontend/presentation/ui/primitives.js';
import '../../../common/frontend/presentation/display-format.js';

import '../presentation/listener/rdio-display.js';

import { connectFields, findAll } from './dom-shim.js';

import { getAppContext } from '../application/context.js';

import { formatTime, progressDate } from '../../../common/frontend/presentation/display-format.js';

const CALL = {
  id: 7,
  system: 1,
  talkgroup: 10,
  talkgroupData: { label: 'Fire dispatch', name: 'Fire dispatch' },
  systemData: { label: 'Metro' },
  frequency: 155000,
  dateTime: 1721692800000,
  audio: { data: [1] },
};

let el;
let ticks;
let realSetInterval;
let realClearInterval;
let realAudio;

beforeEach(() => {
  document.body.replaceChildren();

  const context = getAppContext();

  realAudio = context.audio;

  let elapsed = null;
  context.audio = {
    playbackElapsed: () => elapsed,
    setElapsed: (seconds) => {
      elapsed = seconds;
    },
  };

  ticks = [];
  realSetInterval = globalThis.setInterval;
  realClearInterval = globalThis.clearInterval;
  globalThis.setInterval = (cb, delay) => {
    ticks.push({ cb, delay });
    return 0;
  };
  globalThis.clearInterval = () => {};

  el = document.createElement('rdio-display');
  el.connectedCallback();
  connectFields(el);
});

afterEach(() => {
  el.disconnectedCallback();
  globalThis.setInterval = realSetInterval;
  globalThis.clearInterval = realClearInterval;
  getAppContext().audio = realAudio;
});

const progressTick = () => ticks.filter((t) => t.delay === 500)[0].cb;

const progressText = () =>
  findAll(el, (node) => node.tag === 'span' && node.className.split(/\s+/).includes('call-progress'))[0].textContent;

describe('presentation/listener/rdio-display — the progress row rides the audio clock', () => {
  const reading = (elapsed) => formatTime(progressDate(CALL.dateTime, elapsed));

  it('steps with the engine clock, holds while the clock is frozen, and restarts on a new recording', () => {
    const { store } = getAppContext();
    store.setState({ ...store.getState(), call: CALL });

    const tick = progressTick();
    const audio = getAppContext().audio;

    audio.setElapsed(0);
    tick();
    assert.equal(progressText(), reading(0), 'a fresh recording starts at the call start');

    audio.setElapsed(65);
    tick();
    assert.equal(progressText(), reading(65), 'the row steps with the recording, not with wall time');

    tick();
    assert.equal(progressText(), reading(65), 'a frozen clock — a suspended context — holds the reading');

    audio.setElapsed(130);
    tick();
    assert.equal(progressText(), reading(130), 'the recording continues where it froze');

    audio.setElapsed(0);
    tick();
    assert.equal(progressText(), reading(0), 'a new recording restarts the row at the call start');
  });
});
