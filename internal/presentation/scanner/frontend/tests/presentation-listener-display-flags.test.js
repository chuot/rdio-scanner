// The flag row says which talkgroups the listener has taken out of the live feed, and for how long.
//
// The timed-avoid flag is the one member of that row that is drawn instead of spelled: the window
// the livefeed map holds becomes a glyph with the minutes beside it. The v6.6.3 display drew that
// glyph as a monochrome symbol, so it took the pale cyan the flag row is written in; the port draws
// U+231B with U+FE0F, the selector that asks the browser for the colour-emoji face and refuses the
// text face, and a full-colour pictogram lands in the middle of a row of flat text.
//
// The readings are taken on the rendered display: the flag's own text after a call and a livefeed
// map have been put through the store the display is bound to. The state the glyph rides on — which
// window the map holds, whether the talkgroup is avoided at all, whether a patch keeps the flag
// lit — is read here too, so a change to the glyph cannot quietly change what the flag states.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { afterEach, beforeEach, describe, it } from 'node:test';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/listener/rdio-display.js';

import { connectFields, findAll } from './dom-shim.js';

import { getAppContext } from '../application/context.js';

// The glyph the flag is meant to draw, and the pair it draws today. Written as escapes because the
// emoji-presentation selector is invisible, and a test that cannot name what it is rejecting cannot
// tell the reader why it failed.
const HOURGLASS = '\u29D6';
const EMOJI_HOURGLASS = '\u231B';
const EMOJI_PRESENTATION = '\uFE0F';

const CALL = {
  id: 7,
  system: 1,
  talkgroup: 10,
  talkgroupData: { label: 'Fire dispatch', name: 'Fire dispatch' },
  systemData: { label: 'Metro' },
  frequency: 155000,
  dateTime: Date.now(),
};

// The display runs a clock and a progress tick, so it is mounted in beforeEach and closed in
// afterEach — including when a case fails, which is why the closing is not left to the case itself.
// The store the context carries is shared by every case in the file, so the keys this suite writes
// are cleared on both sides of it: no case inherits another's call or map.
let el;

const store = () => getAppContext().store;

beforeEach(() => {
  store().setState({ call: null, map: undefined });

  el = document.createElement('rdio-display');

  el.connectedCallback();
  connectFields(el);
});

afterEach(() => {
  el.disconnectedCallback();
  store().setState({ call: null, map: undefined });
});

const span = (name) =>
  findAll(el, (node) => node.tag === 'span' && node.className.split(/\s+/).includes(name))[0];

// The reading the operator takes: the text the flag shows for a call on system 1 / talkgroup 10
// against a livefeed map, and the classes that say which flags are lit.
function flagsFor(map) {
  store().setState({ call: CALL, map });

  return {
    tempAvoid: span('temp-avoid').textContent,
    lit: ['temp-avoid', 'flag-avoid', 'flag-patch', 'flag-delayed'].filter((name) => span(name).classList.contains('flaged')),
  };
}

describe('presentation/listener/rdio-display — the timed-avoid flag', () => {
  it('draws the timed window as a monochrome hourglass beside the minutes', () => {
    const { tempAvoid } = flagsFor({ 1: { 10: { active: false, minutes: 30 } } });

    assert.equal(tempAvoid, `${HOURGLASS} 30M`,
      'the timed-avoid flag does not read as the monochrome hourglass and the minutes it carries');
    assert.ok(!tempAvoid.includes(EMOJI_HOURGLASS),
      'the timed-avoid flag still draws U+231B, the emoji hourglass, instead of the monochrome symbol');
    assert.ok(!tempAvoid.includes(EMOJI_PRESENTATION),
      'the timed-avoid flag still carries U+FE0F, the selector that asks the browser for the colour-emoji face');
  });
});
