// Admin config WebSocket adapter (legacy admin.service.ts configWebSocket*).
// Handshake: raw JWT as the first text message. Server pushes the full Config
// JSON on every change. Close 1000 → session ended; other codes → reconnect
// after 2000 ms.

export const RECONNECT_DELAY_MS = 2000;

export function createAdminSocket({ url, token, WebSocketImpl = globalThis.WebSocket, setTimeoutImpl = globalThis.setTimeout }) {
  let websocket = null;

  const subscribers = new Set();

  const emit = (event) => {
    for (const callback of subscribers) callback(event);
  };

  const open = () => {
    if (!token) return;

    websocket = new WebSocketImpl(url);

    websocket.onopen = () => {
      websocket.send(token);

      websocket.onmessage = (ev) => {
        emit({ type: 'config', config: JSON.parse(ev.data) });
      };
    };

    websocket.onclose = (ev) => {
      if (ev.code === 1000) {
        websocket = null;
        emit({ type: 'unauthorized' });
      } else {
        setTimeoutImpl(() => open(), RECONNECT_DELAY_MS);
      }
    };
  };

  return {
    connect: open,

    close() {
      if (!websocket) return;

      websocket.onopen = null;
      websocket.onclose = null;
      websocket.onmessage = null;

      websocket.close();
      websocket = null;
    },

    subscribe(callback) {
      subscribers.add(callback);
      return () => subscribers.delete(callback);
    },
  };
}
