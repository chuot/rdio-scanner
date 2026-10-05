// Application context: single composition point wiring the ports and the
// listener use-cases. Imported by listener.js and the listener elements.

import { createStorage } from '../infrastructure/storage.js';
import { createListenerSocket } from '../infrastructure/listener-socket.js';
import { createAudioEngine } from '../infrastructure/audio-engine.js';
import { createStore } from './store.js';
import { createListenerUseCases } from './listener-use-cases.js';

export function parseInstanceId(url) {
  const params = new URL(url).searchParams;
  return params.get('id') || 'default';
}

export function createAppContext({ location, localStorage, sessionStorage, document }) {
  const instanceId = parseInstanceId(location.href);

  const storage = createStorage({ localStorage, sessionStorage });

  if (location.pathname.endsWith('/reset')) {
    storage.clearAll();

    location.href = location.pathname.replace(/\/reset\/?$/, '/');
  }

  const socket = createListenerSocket({
    url: location.href.replace(/^http/, 'ws').replace(/\/reset\/?$/, '/'),
  });

  const audio = createAudioEngine({ documentImpl: document });

  const store = createStore({
    auth: { status: 'anonymous', denied: false },
    categories: [],
    config: null,
    holdSys: false,
    holdTg: false,
    linked: false,
    listeners: 0,
    livefeedMode: 'offline',
    map: {},
    pause: false,
    playbackList: null,
    queue: 0,
  });

  const useCases = createListenerUseCases({ socket, storage, audio, store, instanceId });

  audio.bootstrap();

  socket.connect();

  return { audio, instanceId, socket, storage, store, useCases };
}

let context = null;

export function getAppContext() {
  if (!context) {
    context = createAppContext({
      location: window.location,
      localStorage: window.localStorage,
      sessionStorage: window.sessionStorage,
      document: window.document,
    });
  }

  return context;
}
