// domain/random-generator: the access code and the API key a generate control draws.
// Every byte the generator sees comes from the source the reading hands it, which is what lets a reading
// name the bytes, their order, and how many times the draw asked for one.

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { generateAccessCode, generateApiKey } from '../domain/random-generator.js';

// The 67 characters the server's own generator declares: secretAlphabet in internal/domain/options.go —
// 26 lower, 26 upper, 10 digits, and the five specials ! # $ % &. 67 is not a power of two, so a uniform
// byte has no even place in a span of it: 3 * 67 = 201, and a byte at 201 or above is one the draw has to
// drop rather than fold in.
const ACCESS_CODE_ALPHABET = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$%&';
const ACCESS_CODE_LENGTH = 10;
const EVEN_DRAW_CEILING = 200;
// 256 = 3 * 67 + 55, so 55 of the characters can be reached by four byte values each and these twelve —
// the digits 3 to 9 and the five specials — by three. They are what a byte remainder starves.
const STARVED = ['3', '4', '5', '6', '7', '8', '9', '!', '#', '$', '%', '&'];
// 60 000 codes of ten positions is 600 000 draws. 8955 per character is what an even draw of that many
// gives, and the band is the one research.md measured a rejection draw against: an even draw never left it,
// and byte % 67 put all twelve starved characters below it at 6936 to 7216.
const CODE_COUNT = 60000;
const EXPECTED_COUNT = 8955;
const COUNT_FLOOR = 8673;
const COUNT_CEILING = 9237;
const UUID_FORM = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
// A reading has to be able to see a draw that kept asking instead of giving up, so every source here stops
// being difficult past ASK_LIMIT bytes and starts handing ordinary ones: a generator with no end to its
// drawing returns a value, fails the reading it was meant to refuse, and leaves the suite running.
const ASK_LIMIT = 10000;
const ORDINARY_BYTES = Array.from({ length: EVEN_DRAW_CEILING + 1 }, (_, byte) => byte);

// mulberry32: the seed the uniform stream below shuffles its blocks with. It is not part of anything the
// generator draws — no reading needs a second source of bytes, only a fixed order of them.
function seededOrder(seed) {
  let state = seed >>> 0;

  return () => {
    state = (state + 0x6d2b79f5) >>> 0;
    let mixed = state;
    mixed = Math.imul(mixed ^ (mixed >>> 15), mixed | 1);
    mixed ^= mixed + Math.imul(mixed ^ (mixed >>> 7), mixed | 61);

    return ((mixed ^ (mixed >>> 14)) >>> 0) / 4294967296;
  };
}

// uniformSource hands a stream in which every block of 256 bytes holds each byte value exactly once, in an
// order the seed fixes. It is uniform by construction rather than by sampling, and the same seed answers the
// same stream on every run, so a reading that counts draws cannot turn red on sampling noise. A block is
// shuffled as the stream reaches it, so a long reading never wraps back into its own beginning.
function uniformSource(seed = 0x9e3779b9) {
  const nextOrder = seededOrder(seed);
  let block = [];
  let cursor = 0;
  let given = 0;

  const shuffleBlock = () => {
    block = Array.from({ length: 256 }, (_, byte) => byte);

    for (let index = 255; index > 0; index--) {
      const swapWith = Math.floor(nextOrder() * (index + 1));
      const held = block[index];

      block[index] = block[swapWith];
      block[swapWith] = held;
    }

    cursor = 0;
  };

  shuffleBlock();

  return {
    given: () => given,
    getRandomValues(target) {
      for (let index = 0; index < target.length; index++) {
        if (cursor === 256) shuffleBlock();

        target[index] = block[cursor];
        cursor++;
        given++;
      }

      return target;
    },
  };
}

// scriptSource hands out the named bytes in the order given and then repeats them, so a reading controls
// every byte the generator is handed without pinning how many it is asked for at a time. given() counts the
// bytes handed, which is how a reading sees a draw that asked again, or one that asked forever.
function scriptSource(script) {
  let given = 0;

  return {
    given: () => given,
    getRandomValues(target) {
      for (let index = 0; index < target.length; index++) {
        const feeding = given <= ASK_LIMIT ? script : ORDINARY_BYTES;

        target[index] = feeding[given % feeding.length];
        given++;
      }

      return target;
    },
  };
}

// deadSource cannot draw at all. Past the same bound it starts answering, for the same reason a scripted
// source does: a generator that swallowed the refusal and kept trying has to hand a value and fail, not
// keep the suite waiting.
function deadSource() {
  let asks = 0;
  let answering = null;

  return {
    asked: () => asks,
    getRandomValues(target) {
      asks++;

      if (asks > ASK_LIMIT) {
        answering = answering ?? uniformSource(0x5bd1e995);

        return answering.getRandomValues(target);
      }

      throw new Error('the platform entropy source cannot draw');
    },
  };
}

const EVEN_DRAW_BYTES = Array.from({ length: EVEN_DRAW_CEILING + 1 }, (_, byte) => byte);
const STARVED_BYTES = Array.from({ length: 256 - EVEN_DRAW_CEILING - 1 }, (_, index) => EVEN_DRAW_CEILING + 1 + index);

function spreadOf(counts) {
  return [...counts.entries()]
    .sort((first, second) => first[1] - second[1])
    .map(([character, count]) => `${character}=${count}`)
    .join(' ');
}

describe('domain/random-generator — generateAccessCode', () => {
  it('draws ten positions, each one of the 67 characters the server draws from and nothing else', () => {
    const source = uniformSource();

    for (let draw = 0; draw < 2000; draw++) {
      const code = generateAccessCode(source, []);

      assert.equal(typeof code, 'string', `a drawn access code is a string, not ${typeof code}`);
      assert.equal(
        code.length,
        ACCESS_CODE_LENGTH,
        `a drawn access code holds ${code.length} positions, not ${ACCESS_CODE_LENGTH}: ${code}`
      );

      for (const character of code) {
        assert.ok(
          ACCESS_CODE_ALPHABET.includes(character),
          `${character} is not one of the 67 characters secretAlphabet declares: ${code}`
        );
      }
    }
  });

  it('reaches every one of the 67 characters, including the twelve a byte remainder starves', () => {
    const source = scriptSource(EVEN_DRAW_BYTES);
    const drawn = new Set();

    for (let draw = 0; draw < 2000; draw++) {
      for (const character of generateAccessCode(source, [])) drawn.add(character);
    }

    assert.deepEqual(
      [...ACCESS_CODE_ALPHABET].filter((character) => !drawn.has(character)),
      [],
      'these characters of the declared alphabet were never drawn'
    );
  });

  it('asks only the source it is handed: the same bytes draw the same code, on every call', () => {
    // Every byte here sits inside 0 … 200, so an even draw uses each of them, and each one lands on the
    // character its position in three copies of the 67 gives it.
    const script = [4, 17, 66, 200, 133, 67, 0, 25, 51, 52, 61, 62, 65, 12, 7];
    const drawn = [generateAccessCode(scriptSource(script), []), generateAccessCode(scriptSource(script), []), generateAccessCode(scriptSource(script), [])];

    assert.equal(drawn[1], drawn[0], `the same bytes drew ${drawn[0]} and then ${drawn[1]}, so another source fed the draw`);
    assert.equal(drawn[2], drawn[0], `the same bytes drew ${drawn[0]} and then ${drawn[2]} on the next call`);
    assert.equal(drawn[0], 'er&&&aazZ0', `the bytes drawn are not the characters they sit on in the span: ${drawn[0]}`);
  });

  it('drops a byte with no even place in the span rather than folding it in with a remainder', () => {
    // Ten bytes with no even place, each followed by one that has: an even draw hands over the ten that
    // have one, in order. A draw that folds every byte with a remainder writes them as pairs instead.
    const script = [201, 0, 202, 1, 203, 2, 204, 3, 205, 4, 206, 5, 207, 6, 208, 7, 209, 8, 210, 9];

    assert.equal(generateAccessCode(scriptSource(script), []), 'abcdefghij', 'a folded byte remainder wrote a different code than the ten bytes with an even place in the span');
  });

  it('asks nothing of Math.random: every draw completes with it replaced by a source that throws', () => {
    const realRandom = Math.random;
    const source = uniformSource();

    Math.random = () => {
      throw new Error('this reading hands out no Math.random');
    };

    try {
      for (let draw = 0; draw < 500; draw++) {
        assert.equal(generateAccessCode(source, []).length, ACCESS_CODE_LENGTH, 'an access code draw stopped short without Math.random');
      }

      assert.equal(generateApiKey(source).length, 36, 'an API key draw stopped short without Math.random');
    } finally {
      Math.random = realRandom;
    }
  });

  it('refuses a source that can only hand bytes with no even place in the span, rather than folding them in', () => {
    const source = scriptSource(STARVED_BYTES);

    assert.equal(
      generateAccessCode(source, []),
      null,
      `a source handing only the bytes ${STARVED_BYTES[0]} to 255 has nothing an even draw can use, and folding them in is not a way to answer that`
    );
    assert.ok(source.given() <= ASK_LIMIT, `the draw handed itself ${source.given()} bytes before it refused: it was still drawing instead of giving up`);
  });

  it('refuses a source that cannot draw at all, and hands back nothing to write', () => {
    const source = deadSource();

    assert.equal(generateAccessCode(source, []), null, 'a source that cannot draw was answered some other way');
    assert.ok(source.asked() <= ASK_LIMIT, `the draw asked a refusing source ${source.asked()} times before it gave up`);
  });

  it('draws again rather than writing a code another row of the section already holds', () => {
    const source = uniformSource();
    const held = [generateAccessCode(source, [])];
    const drawn = generateAccessCode(source, held);

    assert.ok(drawn !== null, `the draw refused rather than drawing a second time to avoid ${held[0]}`);
    assert.ok(!held.includes(drawn), `the draw wrote a code another row of the section holds: ${drawn}`);
    assert.equal(drawn.length, ACCESS_CODE_LENGTH, 'the code drawn again is not a code of the stated length');
  });

  it('refuses rather than looping when the only code the source can draw is one the section holds', () => {
    // Every byte here is the span's first, so the only code this source can ever answer is ten of its
    // first character, and that is the one the caller named as held.
    const source = scriptSource([0]);
    const held = ['a'.repeat(ACCESS_CODE_LENGTH)];

    assert.equal(generateAccessCode(source, held), null, `the source can only ever draw ${held[0]}, which the caller named as held, and drawing it again is not an answer`);
    assert.ok(source.given() <= ASK_LIMIT, `the draw handed itself ${source.given()} bytes before it refused: it was still drawing instead of giving up`);
  });

  it('counts every character over 60 000 codes with none favoured or starved beyond the band', () => {
    const source = uniformSource();
    const counts = new Map();

    for (let draw = 0; draw < CODE_COUNT; draw++) {
      for (const character of generateAccessCode(source, [])) counts.set(character, (counts.get(character) ?? 0) + 1);
    }

    assert.equal(counts.size, ACCESS_CODE_ALPHABET.length, `the draw produced ${counts.size} distinct characters, not the ${ACCESS_CODE_ALPHABET.length} the span holds`);

    assert.deepEqual(
      [...counts.entries()].filter(([, count]) => count < COUNT_FLOOR || count > COUNT_CEILING).map(([character, count]) => `${character}=${count}`),
      [],
      `${spreadOf(counts)}: over ${CODE_COUNT} codes no character may fall outside ${COUNT_FLOOR} to ${COUNT_CEILING} around the ${EXPECTED_COUNT} an even draw gives`
    );

    const starvedCounts = STARVED.map((character) => counts.get(character) ?? 0);

    assert.ok(
      starvedCounts.every((count) => count >= COUNT_FLOOR),
      `the twelve characters a byte remainder starves (${STARVED.join(' ')}) counted ${starvedCounts.join(' ')}: the draw favoured the other ${ACCESS_CODE_ALPHABET.length - STARVED.length}`
    );
  });
});

describe('domain/random-generator — generateApiKey', () => {
  it('draws a version-4 UUID in the canonical 8-4-4-4-12 lower-case form', () => {
    const source = uniformSource();

    for (let draw = 0; draw < 2000; draw++) {
      const key = generateApiKey(source);

      assert.equal(typeof key, 'string', `a drawn API key is a string, not ${typeof key}`);
      assert.match(key, UUID_FORM, `a drawn API key is not the form crypto.randomUUID writes: ${key}`);
      assert.equal(key.length, 36, `a drawn API key is ${key.length} characters, not 36: ${key}`);
      assert.ok(!/\s/.test(key), `a drawn API key carries whitespace, which a form upload cannot carry: ${JSON.stringify(key)}`);
    }
  });

  it('formats the bytes it drew, in the order drawn, and sets the version and variant nibbles itself', () => {
    const drawn = [0x00, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x02, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde];

    assert.equal(
      generateApiKey(scriptSource(drawn)),
      '00012345-6789-4bcd-af02-3456789abcde',
      'the bytes drawn are the key, in the order they were drawn, with only the version and variant nibbles set'
    );
    assert.equal(
      generateApiKey(scriptSource(new Array(16).fill(0x00))),
      '00000000-0000-4000-8000-000000000000',
      'the version nibble is 4 and the variant nibble is set to 8 even when the bytes drawn say otherwise'
    );
    assert.equal(
      generateApiKey(scriptSource(new Array(16).fill(0xff))),
      'ffffffff-ffff-4fff-bfff-ffffffffffff',
      'the variant nibble is set to b rather than copied from the drawn byte, and the hexadecimal is lower-case'
    );
  });

  it('draws a different key on every press', () => {
    const source = uniformSource();
    const keys = new Set();

    for (let draw = 0; draw < 2000; draw++) keys.add(generateApiKey(source));

    assert.equal(keys.size, 2000, 'a press drew a key another press had already drawn');
  });

  it('refuses a source that cannot draw at all, and hands back nothing to write', () => {
    const source = deadSource();

    assert.equal(generateApiKey(source), null, 'a source that cannot draw was answered some other way');
    assert.ok(source.asked() <= ASK_LIMIT, `the draw asked a refusing source ${source.asked()} times before it gave up`);
  });

  it('draws again rather than writing a key another row of the section already holds', () => {
    const source = uniformSource();
    const held = [generateApiKey(source)];
    const drawn = generateApiKey(source, held);

    assert.ok(drawn !== null, `the draw refused rather than drawing a second time to avoid ${held[0]}`);
    assert.ok(!held.includes(drawn), `the draw wrote a key another row of the section holds: ${drawn}`);
    assert.match(drawn, UUID_FORM, 'the key drawn again is not a key of the stated form');
  });

  it('refuses rather than looping when the only key the source can draw is one the section holds', () => {
    // Every byte here is the same, so this source answers one key however often it is asked, and that key
    // is the one the caller named as held.
    const source = scriptSource([0x2a]);
    const held = [generateApiKey(source)];

    assert.ok(held[0] !== null, 'the source answered the first time it was asked, which is what this reading starts from');
    assert.equal(generateApiKey(source, held), null, `the source can only ever draw ${held[0]}, which the caller named as held, and drawing it again is not an answer`);
    assert.ok(source.given() <= ASK_LIMIT, `the draw handed itself ${source.given()} bytes before it refused: it was still drawing instead of giving up`);
  });
});
