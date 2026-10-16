// Listener orchestration use-cases: the single owner of live-listener state,
// replacing the legacy RdioScannerService god-service. Coordinates the socket,
// storage, audio engine, store, and the pure domain modules.

import { normalizeListenerConfig, transformCall } from '../../../common/frontend/domain/config.js';
import {
    avoidMinutes,
    decodeMap,
    encodeMap,
    isAvoided,
    rebuildMap,
    toggleAll as toggleAllMap,
    setSystemStatus as setSystemStatusMap,
    toggleTalkgroup,
} from '../domain/livefeed-map.js';
import { deriveCategories, toggleCategory as toggleCategoryMap } from '../domain/categories.js';
import { expireAvoid, nextAvoidStep } from '../domain/avoid.js';
import { resolveAlertSequence } from '../domain/alert-priority.js';
import { holdSystemMap, holdTalkgroupMap } from '../domain/hold.js';
import { enqueue, filterActive, playbackQueueCount as playbackQueueCountOf } from '../domain/playback-queue.js';
import { initialAuthState, nextAuthState, readPin } from '../domain/pin.js';
import { PLAYBACK_GAP_MS } from '../infrastructure/audio-engine.js';

export const LivefeedMode = { Offline: 'offline', Online: 'online', Playback: 'playback' };

export function createListenerUseCases({
  socket,
  storage,
  audio,
  store,
  instanceId = 'default',
  setTimeoutImpl = globalThis.setTimeout,
  clearTimeoutImpl = globalThis.clearTimeout,
}) {
  let config = null;
  let livefeedMap = {};
  let categories = [];
  let livefeedMode = LivefeedMode.Offline;
  let livefeedPaused = false;

  let call = null;
  let callPrevious = null;
  let callQueue = [];
  let skipDelay = null;
  let playbackToken = 0;
  let activeHandle = null;

  let playbackList = null;
  let playbackListSeq = 0;
  let playbackPending = undefined;
  let playbackRefreshing = false;

  let priorToHoldSystem = null;
  let priorToHoldTalkgroup = null;

  let avoidTimers = new Map();

  let auth = initialAuthState();
  let listeners = 0;

  const emit = (event) => store.setState(event);

  // — livefeed map plumbing —

  const saveMap = () => storage.writeLivefeedMap(instanceId, encodeMap(livefeedMap));

  const rebuildCategories = () => {
    categories = deriveCategories(config?.groups ?? {}, livefeedMap);
  };

  const sendMap = () => {
    if (livefeedMode === LivefeedMode.Online) socket.send('LFM', encodeMap(livefeedMap));
  };

  const cleanQueue = () => {
    callQueue = filterActive(callQueue, livefeedMap);

    if (call && !filterActive([call], livefeedMap).length) skip();
  };

  const clearHoldPriors = () => {
    priorToHoldSystem = null;
    priorToHoldTalkgroup = null;
  };

  function applyMapOptions(systemRef, talkgroupRef, options) {
    livefeedMap = toggleTalkgroup(livefeedMap, systemRef, talkgroupRef, options);

    const key = `${systemRef}/${talkgroupRef}`;
    if (avoidTimers.has(key)) {
      clearTimeoutImpl(avoidTimers.get(key));
      avoidTimers.delete(key);
    }

    const minutes = livefeedMap?.[systemRef]?.[talkgroupRef]?.minutes;
    if (typeof minutes === 'number') {
      avoidTimers.set(key, setTimeoutImpl(() => {
        avoidTimers.delete(key);
        livefeedMap = expireAvoid(livefeedMap, systemRef, talkgroupRef);
        rebuildCategories();
        saveMap();
        sendMap();
        emit({ map: livefeedMap, categories });
      }, minutes * 60 * 1000));
    }
  }

  // — playback —

  function play(next) {
    if (livefeedPaused || skipDelay) return;

    // A call handed in is one the listener asked to hear again or opened from the search panel; a
    // call taken from the queue is an arrival. Only an arrival carries an alert tone.
    const arrival = !next?.audio;

    if (next?.audio) {
      if (call) stop({ emit: false });
      call = next;
    } else if (call) {
      return;
    } else {
      call = callQueue.shift();
    }

    if (!call?.audio) {
      call = null;
      return;
    }

    // The queue number is the operator's live reading of the waiting calls: it moves the moment
    // the queue moves — a call taken out for its tone has left it — and the number carried when
    // a recording starts is the queue as it stands then, counting any call that arrived while
    // the tone was open. Only the recording waits on the tone; the queue never does.
    const liveQueue = () => (livefeedMode === LivefeedMode.Playback ? playbackQueueCount() : callQueue.length);

    emit({ queue: liveQueue() });

    const tone = arrival && livefeedMode === LivefeedMode.Online ? resolveAlertSequence(call, config) : undefined;

    const token = playbackToken;

    const start = () => {
      // A tone opens a window: the call can be skipped, stopped, taken out of the selection, or
      // left behind by the queue before its recording begins. Only the call the listener is still
      // hearing starts one.
      if (token !== playbackToken) return;

      // The handle is kept because the stop funnel uses it: a recording only goes silent when
      // its handle is stopped. The ended closure carries this play's token so a recording that
      // was silenced — whose `ended` a browser still fires on a manual stop — cannot trigger
      // the skip it was wired to.
      activeHandle = audio.playCall(call, {
        onEnded: () => {
          if (token === playbackToken) skip({ delay: true });
        },
      });

      emit({ call, queue: liveQueue() });
    };

    if (tone) audio.playOscillators(tone).then(start);
    else start();
  }

  function playbackQueueCount() {
    if (!playbackList || !call) return 0;

    return playbackQueueCountOf(playbackList, call.id);
  }

  function stop(options = {}) {
    // Every action that ends a call moves this, including the ones that happen while a tone is
    // still playing, so a recording that was waiting on its tone knows it is no longer wanted.
    playbackToken += 1;

    if (activeHandle) {
      activeHandle.stop();
      activeHandle = null;
    }

    if (call) {
      callPrevious = call;
      call = null;
    }

    if (options.emit !== false) emit({ call: null });
  }

  function skip(options = {}) {
    const next = () => {
      if (livefeedMode === LivefeedMode.Playback) playbackNextCall();
      else play();
    };

    stop();

    if (options.delay) {
      skipDelay = setTimeoutImpl(() => {
        skipDelay = null;
        next();
      }, PLAYBACK_GAP_MS);
    } else {
      if (skipDelay) {
        clearTimeoutImpl(skipDelay);
        skipDelay = null;
      }
      next();
    }
  }

  function playbackNextCall() {
    if (call || livefeedMode !== LivefeedMode.Playback || !playbackList || playbackPending) return;

    const { count, options, results } = playbackList;
    const limit = options?.limit ?? results.length;
    const offset = options?.offset ?? 0;

    const index = results.findIndex((c) => c.id === callPrevious?.id);

    const refresh = () => {
      if (playbackRefreshing) {
        stopPlaybackMode();

        // Exhaustion always goes live: the retired playback-goes-live
        // option is gone, and the listener returns to the live feed.
        startLivefeed();
      } else {
        playbackRefreshing = true;
        searchCalls(playbackList.options);
      }
    };

    // Playback walks toward the newest call of the searched history in both sort orders. A
    // descending page keeps its newest call in its first row, so the walk runs back through the
    // page and on to the page of newer calls the server has behind it; an ascending page runs
    // forward through the page and on to the page of newer calls after it. A reference the page
    // does not hold — a page the walk stepped into, or one the search redrew — resumes at the
    // row the walk starts from.
    if (!results.length) {
      refresh();
    } else if (options?.sort === -1) {
      if (index < 0) {
        loadAndPlay(results[results.length - 1].id);
      } else if (index === 0) {
        if (offset < limit) refresh();
        else searchCalls({ ...options, offset: offset - limit });
      } else {
        loadAndPlay(results[index - 1].id);
      }
    } else if (index < 0) {
      loadAndPlay(results[0].id);
    } else if (index + 1 >= results.length) {
      if (offset < count - limit) searchCalls({ ...options, offset: offset + limit });
      else refresh();
    } else {
      loadAndPlay(results[index + 1].id);
    }
  }

  // — livefeed modes —

  function startLivefeed() {
    livefeedMode = LivefeedMode.Online;

    emit({ livefeedMode });

    socket.send('LFM', encodeMap(livefeedMap));
  }

  function stopLivefeed() {
    livefeedMode = LivefeedMode.Offline;

    callQueue = [];

    emit({ livefeedMode, queue: 0 });

    stop();

    socket.send('LFM', null);
  }

  function stopPlaybackMode() {
    livefeedMode = LivefeedMode.Offline;

    playbackRefreshing = false;

    callQueue = [];

    emit({ livefeedMode, queue: 0 });

    stop();
  }

  // — public use-cases —

  function livefeed() {
    if (livefeedMode === LivefeedMode.Offline) startLivefeed();
    else if (livefeedMode === LivefeedMode.Online) stopLivefeed();
    else stopPlaybackMode();
  }

  function avoid(callRef) {
    const target = callRef ?? { system: call?.system ?? callPrevious?.system, talkgroup: call?.talkgroup ?? callPrevious?.talkgroup };
    if (target?.system === undefined) return false;

    clearHoldPriors();

    const options = nextAvoidStep({
      avoided: isAvoided(livefeedMap, target.system, target.talkgroup),
      minutes: avoidMinutes(livefeedMap, target.system, target.talkgroup),
    });

    applyMapOptions(target.system, target.talkgroup, options);

    if (livefeedMode !== LivefeedMode.Playback) cleanQueue();

    rebuildCategories();
    saveMap();
    sendMap();

    emit({ categories, holdSys: false, holdTg: false, map: livefeedMap, queue: callQueue.length });

    return isAvoided(livefeedMap, target.system, target.talkgroup);
  }

  function holdSystem(callRef) {
    // legacy holdSystem(): const call = this.call || this.callPrevious
    const held = call ?? callPrevious;
    const target = callRef ?? { system: held?.system, talkgroup: held?.talkgroup };
    if (target?.system === undefined) return;

    if (priorToHoldSystem) {
      livefeedMap = priorToHoldSystem;
      priorToHoldSystem = null;
    } else {
      if (priorToHoldTalkgroup) holdTalkgroup();

      priorToHoldSystem = livefeedMap;
      livefeedMap = holdSystemMap(livefeedMap, target);

      if (livefeedMode !== LivefeedMode.Playback) cleanQueue();
    }

    rebuildCategories();
    saveMap();
    sendMap();

    emit({ categories, holdSys: !!priorToHoldSystem, holdTg: false, map: livefeedMap, queue: callQueue.length });
  }

  function holdTalkgroup(callRef) {
    // legacy holdTalkgroup(): const call = this.call || this.callPrevious
    const held = call ?? callPrevious;
    const target = callRef ?? { system: held?.system, talkgroup: held?.talkgroup };
    if (target?.system === undefined) return;

    if (priorToHoldTalkgroup) {
      livefeedMap = priorToHoldTalkgroup;
      priorToHoldTalkgroup = null;
    } else {
      if (priorToHoldSystem) holdSystem();

      priorToHoldTalkgroup = livefeedMap;
      livefeedMap = holdTalkgroupMap(livefeedMap, target);

      if (livefeedMode !== LivefeedMode.Playback) cleanQueue();
    }

    rebuildCategories();
    saveMap();
    sendMap();

    emit({ categories, holdSys: false, holdTg: !!priorToHoldTalkgroup, map: livefeedMap, queue: callQueue.length });
  }

  function toggleCategory(category) {
    clearHoldPriors();

    livefeedMap = toggleCategoryMap(livefeedMap, config?.systems ?? [], category);

    rebuildCategories();
    saveMap();
    sendMap();
    cleanQueue();

    emit({ categories, holdSys: false, holdTg: false, map: livefeedMap, queue: callQueue.length });
  }

  function toggleAll(status) {
    clearHoldPriors();

    livefeedMap = toggleAllMap(livefeedMap, status);

    rebuildCategories();
    saveMap();
    sendMap();
    cleanQueue();

    emit({ categories, holdSys: false, holdTg: false, map: livefeedMap, queue: callQueue.length });
  }

  // Per-system ALL ON/OFF (legacy avoid({ system, status })).
  function setSystemStatus(systemRef, status) {
    clearHoldPriors();

    livefeedMap = setSystemStatusMap(livefeedMap, systemRef, status);

    rebuildCategories();
    saveMap();
    sendMap();
    cleanQueue();

    emit({ categories, holdSys: false, holdTg: false, map: livefeedMap, queue: callQueue.length });
  }

  function pause(status = !livefeedPaused) {
    livefeedPaused = status;

    if (status) audio.suspend?.();
    else {
      audio.resume?.();
      play();
    }

    emit({ pause: livefeedPaused });
  }

  function replay() {
    if (call?.audio) play(call);
    else if (callPrevious?.audio) play(callPrevious);
  }

  function loadAndPlay(id) {
    if (!id) return;

    if (skipDelay) {
      clearTimeoutImpl(skipDelay);
      skipDelay = null;
    }

    playbackPending = id;

    stop();

    if (livefeedMode === LivefeedMode.Offline) {
      livefeedMode = LivefeedMode.Playback;

      if (priorToHoldSystem) holdSystem();
      if (priorToHoldTalkgroup) holdTalkgroup();

      emit({ livefeedMode, playbackPending: id });
    } else if (livefeedMode === LivefeedMode.Playback) {
      emit({ playbackPending: id });
    }

    socket.send('CAL', `${id}`, 'play');
  }

  function download(id) {
    if (!id) return;

    socket.send('CAL', `${id}`, 'download');
  }

  function searchCalls(options) {
    socket.send('LCL', options);
  }

  function authenticate(pin) {
    auth = nextAuthState(auth, { type: 'pin-submit' });
    emit({ auth });

    socket.send('PIN', btoa(pin));
  }

  function beep(style) {
    const sequence = config?.keypadBeeps?.[style];
    if (sequence) return audio.playOscillators(sequence);

    return Promise.resolve();
  }

  // — socket message handling —

  function queue(incoming, options) {
    if (!incoming?.audio || livefeedMode === LivefeedMode.Offline) return;

    // Defensive client-side mirror of the server's per-client livefeed filter
    // (same active-or-patched semantics as cleanQueue); the legacy client
    // relied on the server alone.
    if (!filterActive([incoming], livefeedMap).length) return;

    callQueue = enqueue(callQueue, incoming, options);

    if (call || callQueue.length > 1 || livefeedPaused || skipDelay) {
      emit({ queue: livefeedMode === LivefeedMode.Online ? callQueue.length : playbackQueueCount() });
    } else {
      play();
    }
  }

  socket.subscribe((event) => {
    if (event.type === 'linked') {
      emit({ linked: true });

      const pin = readPin(storage.local);
      if (pin) authenticate(pin);

      return;
    }

    if (event.type === 'unlinked') {
      emit({ linked: false });
      return;
    }

    if (event.type !== 'message') return;

    switch (event.command) {
      case 'CAL': {
        if (event.payload === null || event.payload === undefined) break;

        if (event.flag === 'download') {
          audio.download(transformCall(config, event.payload));
        } else if (event.flag === 'play' && event.payload.id === playbackPending) {
          playbackPending = undefined;
          queue(transformCall(config, event.payload), { priority: true });
        } else {
          queue(transformCall(config, event.payload));
        }
        break;
      }

      case 'CFG': {
        config = normalizeListenerConfig(event.payload);

        livefeedMap = rebuildMap(config.systems, decodeMap(storage.readLivefeedMap(instanceId)), categories);

        for (const timer of avoidTimers.values()) clearTimeoutImpl(timer);
        avoidTimers.clear();

        saveMap();
        rebuildCategories();

        auth = nextAuthState(auth, { type: 'config' });

        if (livefeedMode === LivefeedMode.Online) startLivefeed();

        emit({ auth, categories, config, holdSys: !!priorToHoldSystem, holdTg: !!priorToHoldTalkgroup, map: livefeedMap });
        break;
      }

      case 'LCL': {
        playbackList = event.payload ?? null;

        // The store deep-compares slices, so an answer equal to the list already on screen
        // notifies nobody. The counter rides every answer so the search panel's progress bar
        // always learns the search ended - identical answers, empty pages, and payloads
        // without a results array alike.
        playbackListSeq += 1;

        if (playbackList && Array.isArray(playbackList.results)) {
          playbackList.results = playbackList.results.map((c) => transformCall(config, c));
        } else if (playbackList) {
          playbackList = null;
        }

        emit({ playbackList, playbackListSeq });

        if (playbackList && livefeedMode === LivefeedMode.Playback) playbackNextCall();

        break;
      }

      case 'LSC':
        listeners = event.payload;
        emit({ listeners });
        break;

      case 'MAX':
        auth = nextAuthState(auth, { type: 'too-many' });
        emit({ auth });
        break;

      case 'PIN':
        auth = nextAuthState(auth, { type: 'pin-prompt' });
        emit({ auth });
        break;

      case 'XPR':
        auth = nextAuthState(auth, { type: 'expired' });
        emit({ auth });
        break;

      case 'VER': {
        const data = event.payload;
        if (config && data !== null && typeof data === 'object') {
          if (typeof data.branding === 'string') config.branding = data.branding;
          if (typeof data.email === 'string') config.email = data.email;

          emit({ config });
        }
        break;
      }
    }
  });

  return {
    avoid,
    authenticate,
    beep,
    download,
    holdSystem,
    holdTalkgroup,
    livefeed,
    loadAndPlay,
    pause,
    play,
    replay,
    searchCalls,
    skip,
    stop,
    stopPlaybackMode,
    setSystemStatus,
    toggleAll,
    toggleCategory,

    get auth() {
      return auth;
    },
    get call() {
      return call;
    },
    get callPrevious() {
      return callPrevious;
    },
    get callQueue() {
      return callQueue;
    },
    get categories() {
      return categories;
    },
    get config() {
      return config;
    },
    get listeners() {
      return listeners;
    },
    get livefeedMode() {
      return livefeedMode;
    },
    get livefeedPaused() {
      return livefeedPaused;
    },
    get livefeedMap() {
      return livefeedMap;
    },
    get holdSys() {
      return !!priorToHoldSystem;
    },
    get holdTg() {
      return !!priorToHoldTalkgroup;
    },
    get playbackList() {
      return playbackList;
    },
    get playbackPending() {
      return playbackPending;
    },
  };
}
