// the flag row says which talkgroups left the live feed and for how
// long; the timed-avoid flag is drawn as a glyph with the minutes
// beside it, and the port asks the browser for its colour-emoji face.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { afterEach, beforeEach, describe, it } from 'node:test';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/listener/rdio-display.js';

import { connectFields, findAll } from './dom-shim.js';

import { getAppContext } from '../application/context.js';

// the glyph the flag is meant to draw, and the pair it draws today,
// written as escapes because the selector is invisible: a test that
// cannot name what it rejects cannot say why it failed.
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

// the display runs a clock and a progress tick, so it is mounted in
// beforeEach and closed in afterEach, including when a case fails.
// the context's store is shared by every case, so its keys are
// cleared on both sides of it.
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

// the reading the operator takes: the flag's text for system 1 and
// talkgroup 10 against a livefeed map, and the classes naming the flags.
function flagsFor(map, call = CALL) {
  store().setState({ call, map });

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

  it('states the window the map holds, whatever length it is', () => {
    const windows = [30, 60, 120].map((minutes) =>
      flagsFor({ 1: { 10: { active: false, minutes } } }).tempAvoid);

    assert.deepEqual(windows, [`${HOURGLASS} 30M`, `${HOURGLASS} 60M`, `${HOURGLASS} 120M`],
      'the timed-avoid flag does not state the window the livefeed map holds');
  });

  it('says nothing for an avoid the map holds with no window', () => {
    const { tempAvoid, lit } = flagsFor({ 1: { 10: { active: false } } });

    assert.equal(tempAvoid, '',
      'the timed-avoid flag states a window for an avoid the livefeed map holds with no window');
    assert.ok(lit.includes('flag-avoid'),
      'an avoided talkgroup does not light the AVOID flag');
  });

  it('keeps the window lit while the avoided talkgroup is patched to an active one', () => {
    const patchedCall = { ...CALL, patches: [20] };
    const { tempAvoid, lit } = flagsFor({ 1: { 10: { active: false, minutes: 30 }, 20: { active: true } } }, patchedCall);

    assert.equal(tempAvoid, `${HOURGLASS} 30M`,
      'the timed-avoid flag loses its window when the avoided talkgroup is patched');
    assert.ok(lit.includes('temp-avoid'),
      'the timed-avoid flag goes dark when the avoided talkgroup is patched');
    assert.ok(lit.includes('flag-patch'),
      'a patch to an active talkgroup does not light the PATCH flag');
  });

  it('lights no flag for a system the livefeed map does not hold', () => {
    const { tempAvoid, lit } = flagsFor(undefined);

    assert.equal(tempAvoid, '', 'the timed-avoid flag states a window for a system the map does not hold');
    assert.deepEqual(lit, [], 'a call the livefeed map knows nothing about lights a flag');
  });
});
