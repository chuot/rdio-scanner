// playback exhaustion goes live: when the playback list runs out, the
// refreshed list is checked once more and the live feed starts. the
// retired playback-goes-live option is gone; exhaustion always starts it.

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { createListenerUseCases } from '../application/listener-use-cases.js';
import { createStore } from '../../../common/frontend/application/store.js';

function createFakeSocket() {
  const sent = [];
  const subscribers = new Set();

  return {
    sent,
    send: (command, payload, flag) => sent.push([command, payload, flag]),
    subscribe: (cb) => {
      subscribers.add(cb);
      return () => subscribers.delete(cb);
    },
    inject: (command, payload, flag) => {
      for (const cb of subscribers) cb({ type: 'message', command, payload, flag });
    },
  };
}

function createFakeStorage() {
  const map = new Map();

  return {
    local: { getItem: () => null, setItem: () => {}, removeItem: () => {}, clear: () => {} },
    readLivefeedMap: () => ({}),
    writeLivefeedMap: () => {},
  };
}

function createFakeAudio() {
  const played = [];

  return {
    played,
    playCall: (call, handlers) => {
      played.push(call);
      call.ended = handlers.onEnded;
      return { stop: () => (call.stopped = true) };
    },
    playOscillators: async () => {},
    download: () => {},
  };
}

function createFakeTimers() {
  const scheduled = [];

  return {
    scheduled,
    setTimeoutImpl: (cb, delay) => {
      const entry = { cb, delay };
      scheduled.push(entry);
      return entry;
    },
    clearTimeoutImpl: (entry) => scheduled.splice(scheduled.indexOf(entry), 1),
    fireAll: () => {
      const pending = [...scheduled];
      scheduled.length = 0;
      pending.forEach((e) => e.cb());
    },
  };
}

const cfgPayload = {
  systems: [{ id: 10, label: 'SYS1', talkgroups: [{ id: 100, label: 'TG1', groups: ['Fire'], tag: 'T1' }] }],
  groups: { Fire: { 10: [100] } },
  groupsData: [{ id: 1, label: 'Fire' }],
  tags: { T1: { 10: [100] } },
  tagsData: [{ id: 2, label: 'T1' }],
};

const oneCall = { id: 1, system: 10, talkgroup: 100, dateTime: 1721692800000, audio: { data: [1] } };

function setup() {
  const socket = createFakeSocket();
  const storage = createFakeStorage();
  const audio = createFakeAudio();
  const timers = createFakeTimers();
  const store = createStore({});

  const useCases = createListenerUseCases({
    socket,
    storage,
    audio,
    store,
    instanceId: 'default',
    ...timers,
  });

  socket.inject('CFG', cfgPayload);

  return { useCases, socket, audio, timers };
}

describe('application/listener-use-cases — playback exhaustion', () => {
  it('an exhausted playback list returns to the live feed', () => {
    const { useCases, socket, audio, timers } = setup();

    // The listener plays one call from the search panel: playback mode.
    useCases.loadAndPlay(1);

    assert.equal(useCases.livefeedMode, 'playback');

    socket.inject('CAL', oneCall, 'play');

    // The search panel answers with the list of that one call.
    socket.inject('LCL', { count: 1, options: { sort: -1, offset: 0 }, results: [oneCall] });

    // The call ends: the playback gap elapses, the list is exhausted, so the
    // use case asks for a refresh.
    audio.played[0].ended();
    timers.fireAll();

    assert.deepEqual(socket.sent.at(-1).slice(0, 2), ['LCL', { sort: -1, offset: 0 }]);

    // The refreshed list is still exhausted: the listener goes live.
    socket.inject('LCL', { count: 1, options: { sort: -1, offset: 0 }, results: [oneCall] });

    assert.equal(useCases.livefeedMode, 'online');
    assert.ok(socket.sent.at(-1)[0] === 'LFM', 'going live must send the livefeed map');
  });
});
