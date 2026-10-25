// Listener WebSocket adapter: wire frames are arrays
// [command, payload?, flag?].

export const RECONNECT_DELAY_MS = 2000;

export function createListenerSocket({ url, WebSocketImpl = globalThis.WebSocket, setTimeoutImpl = globalThis.setTimeout }) {
  let websocket = null;

  const subscribers = new Set();

  const emit = (event) => {
    for (const callback of subscribers) callback(event);
  };

  const detach = (ws) => {
    ws.onopen = null;
    ws.onclose = null;
    ws.onmessage = null;
    ws.onerror = null;
  };

  const open = () => {
    websocket = new WebSocketImpl(url);

    websocket.onopen = () => {
      emit({ type: 'linked' });

      websocket.onmessage = (ev) => {
        let message;
        try {
          message = JSON.parse(ev.data);
        } catch {
          console.warn(`Invalid control message received, ${ev.data}`);
          return;
        }

        if (!Array.isArray(message)) return;

        emit({ type: 'message', command: message[0], payload: message[1], flag: message[2] });
      };

      send('VER');
      send('CFG');
    };

    websocket.onclose = (ev) => {
      emit({ type: 'unlinked' });

      if (ev.code !== 1000) {
        setTimeoutImpl(() => open(), RECONNECT_DELAY_MS);
      }
    };
  };

  function send(command, payload, flag) {
    if (websocket?.readyState !== 1) return;

    const message = [command];

    if (payload) message.push(payload);
    if (flag !== null && flag !== undefined) message.push(flag);

    websocket.send(JSON.stringify(message));
  }

  return {
    connect: open,
    send,

    close() {
      if (!websocket) return;

      detach(websocket);
      websocket.close();
      websocket = null;
    },

    subscribe(callback) {
      subscribers.add(callback);
      return () => subscribers.delete(callback);
    },
  };
}
