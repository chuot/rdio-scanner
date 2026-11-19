// The credentials the administration webapp can draw for an operator: an access code of ten characters, and
// an API key in the form a version-4 UUID takes. Both are drawn from a random source the caller hands in —
// the browser's crypto object at runtime — and a draw that cannot get a usable value out of it returns null
// rather than a weaker value: the caller writes nothing and says so.

// The alphabet the server's own generator draws from (internal/domain/options.go's secretAlphabet): 26
// lowercase, 26 uppercase, 10 digits, and the five specials ! # $ % &. Access codes are drawn over exactly
// these characters so that a code this page offers is one the server already accepts.
const ACCESS_CODE_ALPHABET = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$%&';
const ACCESS_CODE_LENGTH = 10;

// 67 is not a power of two, and 3 * 67 = 201: the bytes 0 through 200 hold three whole copies of the
// alphabet, and a byte at 201 or above has no even place in it. Those are dropped. Taking a remainder of
// every byte modulo 67 instead would leave 55 of the characters reachable by four byte values each and the
// digits 3 to 9 and the five specials reachable by three, which is a credential that quietly favours some
// characters over others.
const EVEN_DRAW_CEILING = 200;

// A version-4 UUID is sixteen bytes, with the version nibble fixed to 4 and the variant nibble fixed to the
// 8 through b range. This module sets both itself, so the key does not depend on a platform call that a
// plain-HTTP origin does not offer.
const API_KEY_BYTES = 16;
const API_KEY_GROUPS = [8, 12, 16, 20];

// A value is drawn again when it turns out to be one the section already holds. One more draw is what a
// duplicate needs, and eight is far past anything a working source can produce by chance: it is here so that
// a source which can only ever answer a held value is refused rather than waited on for ever.
const MAX_VALUE_ATTEMPTS = 8;

// One generated value asks the source for at most this many bytes. An even draw needs about thirteen bytes
// for a ten-character code, and a draw again needs about thirteen more, so four hundred is far beyond what
// the span can produce by chance. It is what stops a source that answers nothing usable from being asked for
// another byte for ever.
const MAX_SOURCE_BYTES = 400;

const BYTE_BATCH = 16;

// One generated value's worth of drawing, shared by its attempts so a batch is finished before another is
// asked for. Returns the next byte, or null when the source cannot answer — a source that throws, or that
// answers without bytes, is a source that cannot draw, and nothing is invented for it.
function byteDraw(source) {
  let batch = null;
  let cursor = 0;
  let asked = 0;

  return {
    next() {
      if (asked >= MAX_SOURCE_BYTES) return null;

      if (batch === null || cursor >= batch.length) {
        asked += BYTE_BATCH;
        batch = readBatch(source);
        cursor = 0;

        if (batch === null || batch.length === 0) return null;
      }

      return batch[cursor++];
    },
  };
}

function readBatch(source) {
  if (!source || typeof source.getRandomValues !== 'function') return null;

  try {
    return source.getRandomValues(new Uint8Array(BYTE_BATCH));
  } catch {
    return null;
  }
}

function heldSet(heldValues) {
  return new Set(Array.isArray(heldValues) ? heldValues : []);
}

// Draws one access code. Each position is answered by the next byte that has an even place in the span, so
// every character of the alphabet is reachable by the same number of bytes. heldValues names the codes the
// section's other rows already carry: a code among them is drawn again, because saving it would only be
// refused. Returns the code, or null when the source could not answer.
export function generateAccessCode(source, heldValues = []) {
  const held = heldSet(heldValues);
  const draw = byteDraw(source);

  for (let attempt = 0; attempt < MAX_VALUE_ATTEMPTS; attempt++) {
    const characters = [];

    while (characters.length < ACCESS_CODE_LENGTH) {
      const byte = draw.next();

      if (byte === undefined || byte === null) return null;
      if (byte > EVEN_DRAW_CEILING) continue;

      characters.push(ACCESS_CODE_ALPHABET[byte % ACCESS_CODE_ALPHABET.length]);
    }

    const code = characters.join('');

    if (!held.has(code)) return code;
  }

  return null;
}

// Draws one API key: sixteen bytes, formatted as the canonical 8-4-4-4-12 lower-case hexadecimal form of a
// version-4 UUID. Returns the key, or null when the source could not answer.
export function generateApiKey(source, heldValues = []) {
  const held = heldSet(heldValues);
  const draw = byteDraw(source);

  for (let attempt = 0; attempt < MAX_VALUE_ATTEMPTS; attempt++) {
    const bytes = [];

    while (bytes.length < API_KEY_BYTES) {
      const byte = draw.next();

      if (byte === undefined || byte === null) return null;

      bytes.push(byte);
    }

    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;

    const key = formatApiKey(bytes);

    if (!held.has(key)) return key;
  }

  return null;
}

function formatApiKey(bytes) {
  const hex = bytes.map((byte) => byte.toString(16).padStart(2, '0')).join('');
  const groups = [hex.slice(0, API_KEY_GROUPS[0])];

  for (let index = 1; index < API_KEY_GROUPS.length; index++) groups.push(hex.slice(API_KEY_GROUPS[index - 1], API_KEY_GROUPS[index]));

  groups.push(hex.slice(API_KEY_GROUPS[API_KEY_GROUPS.length - 1]));

  return groups.join('-');
}
