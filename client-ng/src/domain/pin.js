// PIN storage and listener auth state machine.
// Legacy: pin persisted base64 under rdio-scanner-pin (window.btoa/atob);
// server sends PIN on restricted/wrong pin, MAX after 5 attempts, XPR on expiry.

export const PIN_STORAGE_KEY = 'rdio-scanner-pin';

export function encodePin(pin) {
  return btoa(pin);
}

export function decodePin(encoded) {
  return atob(encoded);
}

export function readPin(storage) {
  const pin = storage.getItem(PIN_STORAGE_KEY);
  return pin ? decodePin(pin) : undefined;
}

export function savePin(storage, pin) {
  storage.setItem(PIN_STORAGE_KEY, encodePin(pin));
}

export function clearPin(storage) {
  storage.removeItem(PIN_STORAGE_KEY);
}

export function initialAuthState() {
  return { status: 'anonymous', denied: false };
}

export function nextAuthState(state, event) {
  switch (event.type) {
    case 'pin-prompt':
      return { status: 'pin-required', denied: state.status === 'authenticating' };
    case 'pin-submit':
      return { status: 'authenticating', denied: false };
    case 'config':
      return { status: 'authenticated', denied: false };
    case 'too-many':
      return { status: 'too-many', denied: false };
    case 'expired':
      return { status: 'expired', denied: false };
    case 'disconnected':
      return initialAuthState();
    default:
      return state;
  }
}
