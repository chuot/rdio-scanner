// The control buttons' behavior, pinned to the v6.6.3 control row: the locked screen only
// focuses the unlock field and beeps nothing; LIVE FEED beeps activate on the way into the
// offline mode and deactivate on the way out; HOLD SYS and HOLD TG beep denied with no call
// and toggle through the prior maps; REPLAY hands the history walk to the display; SKIP and
// PAUSE carry the legacy beep styles; AVOID cycles through the avoid steps and beeps denied
// with no call; SEARCH and SELECT are gated on the configuration and beep the plain keypad
// beep. The dimmer arms on the controls that armed it in the v6.6.3 control row — every
// control except the two panel openers, and except AVOID when there is no call to avoid.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { afterEach, beforeEach, describe, it } from 'node:test';

import { getAppContext } from '../application/context.js';

import '../presentation/listener/rdio-controls.js';

const SPOOKED = ['beep', 'livefeed', 'skip', 'pause', 'avoid', 'holdSystem', 'holdTalkgroup'];

// The control bar mounted with the use-cases recorded: every beep and every action the bar
// asks for is noted, every event the bar dispatches is noted, and the recording is cleared
// before each press so a case asserts only what its own press did.
function bar() {
  const { store, useCases } = getAppContext();

  const calls = [];
  const events = [];
  const real = {};

  for (const name of SPOOKED) {
    real[name] = useCases[name];

    useCases[name] = (...args) => {
      calls.push([name, ...args]);

      return name === 'avoid' ? bar.avoided : undefined;
    };
  }

  const el = document.createElement('rdio-controls');

  for (const type of ['auth-focus', 'replay', 'open-search', 'open-select', 'dimmer']) {
    el.addEventListener(type, () => events.push(type));
  }

  el.connectedCallback();

  const press = (action) => {
    calls.length = 0;
    events.length = 0;

    el.querySelector(`button[data-action="${action}"]`).click();
  };

  return { store, el, calls, events, press, close: () => el.disconnectedCallback() };
}

let barRef;

beforeEach(() => {
  barRef = bar();

  barRef.store.setState({
    auth: { status: 'anonymous', denied: false },
    call: undefined,
    config: null,
    holdSys: false,
    holdTg: false,
    livefeedMode: 'offline',
    pause: false,
  });
});

afterEach(() => {
  barRef.close();

  const { useCases } = getAppContext();

  for (const [name, fn] of Object.entries(barRef.real ?? {})) useCases[name] = fn;
});

const actions = (calls) => calls.map(([name]) => name);

describe('presentation/listener/rdio-controls — the v6.6.3 control row', () => {
  it('focuses the unlock field and beeps nothing while the screen is locked', () => {
    const { store, events, calls, press } = barRef;

    store.setState({ auth: { status: 'pin-required', denied: false } });

    press('livefeed');

    assert.deepEqual(events, ['auth-focus'], 'a locked control did more than focus the unlock field');
    assert.deepEqual(calls, [], 'a locked control beeped');
  });

  it('beeps LIVE FEED activate on the way offline and deactivate on the way back', () => {
    const { store, calls, events, press } = barRef;

    store.setState({ livefeedMode: 'offline' });

    press('livefeed');

    assert.deepEqual(calls, [['beep', 'activate'], ['livefeed']],
      'the offline livefeed control does not beep activate and arm the livefeed');
    assert.deepEqual(events, ['dimmer'], 'the livefeed control does not arm the dimmer');

    store.setState({ livefeedMode: 'online' });

    press('livefeed');

    assert.deepEqual(calls, [['beep', 'deactivate'], ['livefeed']],
      'the online livefeed control does not beep deactivate');
  });

  it('beeps HOLD SYS by the hold it is in and denied with no call, arming the dimmer on both', () => {
    const { store, calls, events, press } = barRef;

    store.setState({ call: { system: 1, talkgroup: 2 } });

    press('holdSystem');

    assert.deepEqual(calls, [['beep', 'activate'], ['holdSystem']],
      'the unheld HOLD SYS does not beep activate and take the hold');
    assert.deepEqual(events, ['dimmer'], 'HOLD SYS does not arm the dimmer');

    store.setState({ call: { system: 1, talkgroup: 2 }, holdSys: true });

    press('holdSystem');

    assert.deepEqual(calls, [['beep', 'deactivate'], ['holdSystem']],
      'the held HOLD SYS does not beep deactivate');

    store.setState({ call: undefined });

    press('holdSystem');

    assert.deepEqual(calls, [['beep', 'denied']], 'HOLD SYS with no call does not beep denied');
    assert.deepEqual(events, ['dimmer'], 'HOLD SYS denied does not arm the dimmer');
  });

  it('beeps HOLD TG by the hold it is in and denied with no call, arming the dimmer on both', () => {
    const { store, calls, events, press } = barRef;

    store.setState({ call: { system: 1, talkgroup: 2 } });

    press('holdTalkgroup');

    assert.deepEqual(calls, [['beep', 'activate'], ['holdTalkgroup']],
      'the unheld HOLD TG does not beep activate and take the hold');
    assert.deepEqual(events, ['dimmer'], 'HOLD TG does not arm the dimmer');

    store.setState({ call: { system: 1, talkgroup: 2 }, holdTg: true });

    press('holdTalkgroup');

    assert.deepEqual(calls, [['beep', 'deactivate'], ['holdTalkgroup']],
      'the held HOLD TG does not beep deactivate');

    store.setState({ call: undefined });

    press('holdTalkgroup');

    assert.deepEqual(calls, [['beep', 'denied']], 'HOLD TG with no call does not beep denied');
    assert.deepEqual(events, ['dimmer'], 'HOLD TG denied does not arm the dimmer');
  });

  it('hands REPLAY to the history walk and arms the dimmer', () => {
    const { events, calls, press } = barRef;

    press('replay');

    assert.deepEqual(events, ['replay', 'dimmer'], 'REPLAY does not hand the walk to the display and arm the dimmer');
    assert.deepEqual(calls, [], 'REPLAY beeped from the control bar, where the walk names the beep');
  });

  it('beeps SKIP activate, skips, and arms the dimmer', () => {
    const { calls, events, press } = barRef;

    press('skip');

    assert.deepEqual(calls, [['beep', 'activate'], ['skip']], 'SKIP does not beep activate and skip');
    assert.deepEqual(events, ['dimmer'], 'SKIP does not arm the dimmer');
  });

  it('beeps PAUSE by the state the feed is in and arms the dimmer', () => {
    const { store, calls, events, press } = barRef;

    press('pause');

    assert.deepEqual(calls, [['beep', 'activate'], ['pause']],
      'the live feed does not beep activate on the way into the pause');
    assert.deepEqual(events, ['dimmer'], 'PAUSE does not arm the dimmer');

    store.setState({ pause: true });

    press('pause');

    assert.deepEqual(calls, [['beep', 'deactivate'], ['pause']],
      'the paused feed does not beep deactivate on the way back');
  });

  it('beeps AVOID by the avoid it lands in and arms the dimmer', () => {
    const { store, calls, events, press } = barRef;

    store.setState({ call: { system: 1, talkgroup: 2 } });

    bar.avoided = true;

    press('avoid');

    assert.deepEqual(calls, [['avoid'], ['beep', 'activate']],
      'the avoided call does not beep activate');
    assert.deepEqual(events, ['dimmer'], 'AVOID does not arm the dimmer');

    bar.avoided = false;

    press('avoid');

    assert.deepEqual(calls, [['avoid'], ['beep', 'deactivate']],
      'the cleared call does not beep deactivate');
  });

  it('beeps AVOID denied with no call and arms no dimmer', () => {
    const { calls, events, press } = barRef;

    press('avoid');

    assert.deepEqual(calls, [['beep', 'denied']], 'AVOID with no call does not beep denied');
    assert.deepEqual(events, [], 'AVOID with no call armed the dimmer; the v6.6.3 control row leaves the dimmer where it is');
  });

  it('opens SEARCH on the configuration, beeps, and arms no dimmer', () => {
    const { store, calls, events, press } = barRef;

    store.setState({ config: { name: 'field' } });

    press('search');

    assert.deepEqual(calls, [['beep', 'activate']], 'SEARCH does not beep the plain keypad beep');
    assert.deepEqual(events, ['open-search'], 'SEARCH does not open the search panel');

    store.setState({ config: null });

    press('search');

    assert.deepEqual(calls, [], 'SEARCH with no configuration beeped');
    assert.deepEqual(events, [], 'SEARCH with no configuration opened the panel');
  });

  it('opens SELECT on the configuration, beeps, and arms no dimmer', () => {
    const { store, calls, events, press } = barRef;

    store.setState({ config: { name: 'field' } });

    press('select');

    assert.deepEqual(calls, [['beep', 'activate']], 'SELECT does not beep the plain keypad beep');
    assert.deepEqual(events, ['open-select'], 'SELECT does not open the select panel');

    store.setState({ config: null });

    press('select');

    assert.deepEqual(calls, [], 'SELECT with no configuration beeped');
    assert.deepEqual(events, [], 'SELECT with no configuration opened the panel');
  });
});
