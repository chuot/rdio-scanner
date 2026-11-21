// the select panel's presses: a talkgroup press flips the talkgroup
// through the flip use case, never the control bar's timed avoid cycle,
// and beeps by the state the press leaves; a category press beeps by
// the status it leaves. the panel names each toggle's state.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { afterEach, beforeEach, describe, it } from 'node:test';

import { getAppContext } from '../application/context.js';
import { findAll } from './dom-shim.js';

import '../presentation/listener/rdio-select-panel.js';

const SPOOKED = ['beep', 'avoid', 'toggleCategory', 'toggleAll', 'setSystemStatus', 'toggleTalkgroup'];

const CONFIG = {
  systems: [
    { id: 10, label: 'SYS1', talkgroups: [{ id: 100, label: 'TG1' }, { id: 101, label: 'TG2' }] },
  ],
};

const CATEGORIES = [
  { label: 'Fire', status: 'on', type: 'group' },
  { label: 'Police', status: 'off', type: 'group' },
  { label: 'Mixed', status: 'partial', type: 'group' },
];

// The panel mounted on a seeded store with its use-cases recorded: every beep and every action
// the presses ask for is noted, so a case asserts only what its own press did.
function mounted(seedMap) {
  const { store, useCases } = getAppContext();

  const calls = [];
  const real = {};

  for (const name of SPOOKED) {
    real[name] = useCases[name];

    useCases[name] = (...args) => calls.push([name, ...args]);
  }

  store.setState({ config: CONFIG, categories: CATEGORIES, map: seedMap });

  const el = document.createElement('rdio-select-panel');

  el.connectedCallback();

  return { store, el, calls, real, close: () => el.disconnectedCallback() };
}

let panelRef;

beforeEach(() => {
  panelRef = mounted({ 10: { 100: { active: true }, 101: { active: false } } });
});

afterEach(() => {
  panelRef.close();

  const { useCases } = getAppContext();

  for (const [name, fn] of Object.entries(panelRef.real ?? {})) useCases[name] = fn;
});

const talkgroupButton = (el, ref) =>
  findAll(el, (n) => n.tag === 'button' && n.dataset.talkgroup === String(ref))[0];

const categoryButton = (el, label) =>
  findAll(el, (n) => {
    if (n.tag !== 'button' || n.dataset.category === undefined) return false;

    return JSON.parse(decodeURIComponent(n.dataset.category)).label === label;
  })[0];

const miniButton = (el, key, value) =>
  findAll(el, (n) => n.tag === 'button' && n.dataset[key] === value)[0];

const hiddenTexts = (node) => findAll(node, (n) => n.tag === 'span' && n.classList.contains('rdio-visually-hidden')).map((n) => n.textContent);

describe('presentation/listener/rdio-select-panel — the press semantics', () => {
  it('flips an active talkgroup off through the flip use case, beeping deactivate for the state it leaves', () => {
    const { el, calls } = panelRef;

    talkgroupButton(el, 100).click();

    assert.deepEqual(calls, [['beep', 'deactivate'], ['toggleTalkgroup', 10, 100]],
      'the press on an active talkgroup does not beep deactivate and flip it off');
    assert.ok(calls.every(([name]) => name !== 'avoid'),
      'the panel press ran the control bar\'s timed avoid cycle');
  });

  it('flips an inactive talkgroup on through the flip use case, beeping activate for the state it leaves', () => {
    const { el, calls } = panelRef;

    talkgroupButton(el, 101).click();

    assert.deepEqual(calls, [['beep', 'activate'], ['toggleTalkgroup', 10, 101]],
      'the press on an inactive talkgroup does not beep activate and flip it on');
  });

  it('flips a talkgroup in a timed avoid back on, beeping activate', () => {
    const { store, el, calls } = panelRef;

    store.setState({ map: { 10: { 100: { active: false, minutes: 30 }, 101: { active: true } } } });

    talkgroupButton(el, 100).click();

    assert.deepEqual(calls, [['beep', 'activate'], ['toggleTalkgroup', 10, 100]],
      'the press on a timed-avoided talkgroup does not beep activate and flip it on');
  });

  it('beeps a category press by the status it leaves: on deactivates, off and partial activate', () => {
    const { el, calls } = panelRef;

    categoryButton(el, 'Fire').click();

    assert.deepEqual(calls, [['beep', 'deactivate'], ['toggleCategory', CATEGORIES[0]]],
      'the press on an on category does not beep deactivate and toggle it');

    calls.length = 0;
    categoryButton(el, 'Police').click();

    assert.deepEqual(calls, [['beep', 'activate'], ['toggleCategory', CATEGORIES[1]]],
      'the press on an off category does not beep activate and toggle it');

    calls.length = 0;
    categoryButton(el, 'Mixed').click();

    assert.deepEqual(calls, [['beep', 'activate'], ['toggleCategory', CATEGORIES[2]]],
      'the press on a partial category does not beep activate and make all its talkgroups active');
  });

  it('beeps the mini buttons by the status they apply', () => {
    const { el, calls } = panelRef;

    miniButton(el, 'all', 'off').click();

    assert.deepEqual(calls, [['beep', 'deactivate'], ['toggleAll', false]],
      'ALL OFF does not beep deactivate and turn everything off');

    calls.length = 0;
    miniButton(el, 'all', 'on').click();

    assert.deepEqual(calls, [['beep', 'activate'], ['toggleAll', true]],
      'ALL ON does not beep activate and turn everything on');

    calls.length = 0;
    miniButton(el, 'systemOff', '10').click();

    assert.deepEqual(calls, [['beep', 'deactivate'], ['setSystemStatus', 10, false]],
      'the per-system OFF does not beep deactivate and turn the system off');

    calls.length = 0;
    miniButton(el, 'systemOn', '10').click();

    assert.deepEqual(calls, [['beep', 'activate'], ['setSystemStatus', 10, true]],
      'the per-system ON does not beep activate and turn the system on');
  });
});

describe('presentation/listener/rdio-select-panel — the states the panel names', () => {
  it('marks each talkgroup button with the on/off class and the pressed value of the state it holds', () => {
    const { el } = panelRef;

    const on = talkgroupButton(el, 100);
    const off = talkgroupButton(el, 101);

    assert.ok(on.classList.contains('on'), 'the active talkgroup button does not carry the on class');
    assert.equal(on.getAttribute('aria-pressed'), 'true', 'the active talkgroup button does not say it is on');

    assert.ok(off.classList.contains('off'), 'the inactive talkgroup button does not carry the off class');
    assert.equal(off.getAttribute('aria-pressed'), 'false', 'the inactive talkgroup button does not say it is off');
  });

  it('keeps the blink on a timed-avoided talkgroup and states its window in text', () => {
    panelRef.store.setState({ map: { 10: { 100: { active: false, minutes: 30 }, 101: { active: true } } } });

    const timed = talkgroupButton(panelRef.el, 100);

    assert.ok(timed.classList.contains('blink'), 'the timed-avoided talkgroup button does not blink');
    assert.equal(timed.getAttribute('aria-pressed'), 'false', 'the timed-avoided talkgroup button says it is on');
    assert.ok(hiddenTexts(timed).some((text) => text.includes('30')),
      'the blinking talkgroup button does not state its timed window in text');
  });

  it('states each category status in text beside its label', () => {
    const { el } = panelRef;

    const readings = { Fire: 'on', Police: 'off', Mixed: 'partial' };

    for (const [label, status] of Object.entries(readings)) {
      assert.ok(hiddenTexts(categoryButton(el, label)).includes(status),
      `the ${label} category button does not state its ${status} status in text`);
    }
  });
});

// Systems and talkgroups are named by free text an operator saves, and that text
// reaches this panel verbatim in the configuration the server sends each listener.
// A saved label is free to carry the characters markup is built from.
const SYSTEM_LABEL_WITH_MARKUP = {
  systems: [
    { id: 20, label: 'Fire <img src="x" onerror="arm()"> Station', talkgroups: [{ id: 200, label: 'Squad Seven' }] },
  ],
};

const TALKGROUP_LABEL_WITH_MARKUP = {
  systems: [{ id: 21, label: 'Police', talkgroups: [{ id: 210, label: 'Squad <b>Seven</b>' }] }],
};

// A panel mounted on one saved configuration, so each case sees only the label it names.
function mountedPanel(config) {
  const { store } = getAppContext();

  store.setState({ categories: [], config, map: {} });

  const el = document.createElement('rdio-select-panel');

  el.connectedCallback();

  return el;
}

// Anything the panel built out of the label rather than out of its own markup: an
// element the label introduced, or an attribute it introduced on one.
const builtFromTheLabel = (el) =>
  findAll(el, (n) => n.tag === 'img' || n.tag === 'b' || n.tag === 'script' || n.attributes.onerror !== undefined)
    .map((n) => (n.attributes.onerror !== undefined ? `${n.tag}[onerror]` : n.tag));

// A category label reaches the panel the same way: the panel builds the category
// buttons out of its own markup, and shows them only when two or more categories
// are configured.

const CATEGORY_LABEL_WITH_MARKUP = [
  { label: 'Fire <img src="x" onerror="arm()"> Station', status: 'on', type: 'group' },
  { label: 'Police', status: 'off', type: 'group' },
];

describe('presentation/listener/rdio-select-panel — labels a saved configuration carries', () => {
  it('names a system by a label that carries markup, building no element out of that label', () => {
    const el = mountedPanel(SYSTEM_LABEL_WITH_MARKUP);

    try {
      const legend = findAll(el, (n) => n.tag === 'legend')[0];

      assert.ok(legend, 'the panel dropped the system fieldset legend');
      assert.ok(legend.textContent.includes('Station'), 'the legend does not name the system by its label');

      assert.deepEqual(builtFromTheLabel(el), [],
        'the system label built an element or an attribute of its own into the panel');
    } finally {
      el.disconnectedCallback();
    }
  });

  it('names a talkgroup by a label that carries markup, building no element out of that label', () => {
    const el = mountedPanel(TALKGROUP_LABEL_WITH_MARKUP);


    try {
      const button = talkgroupButton(el, 210);

      assert.ok(button, 'the panel dropped the talkgroup button');
      assert.ok(button.textContent.includes('Seven'), 'the talkgroup button does not name the talkgroup by its label');

      assert.deepEqual(builtFromTheLabel(el), [],
        'the talkgroup label built an element or an attribute of its own into the panel');
    } finally {
      el.disconnectedCallback();
    }
  });

  it('names a category by a label that carries markup, building no element out of that label', () => {
    const { store } = getAppContext();

    store.setState({ categories: CATEGORY_LABEL_WITH_MARKUP, config: CONFIG, map: {} });

    const el = document.createElement('rdio-select-panel');

    el.connectedCallback();

    try {
      const button = categoryButton(el, CATEGORY_LABEL_WITH_MARKUP[0].label);

      assert.ok(button, 'the panel dropped the category button');
      assert.ok(button.textContent.includes('Station'), 'the category button does not name the category by its label');

      assert.deepEqual(builtFromTheLabel(el), [],
        'the category label built an element or an attribute of its own into the panel');
    } finally {
      el.disconnectedCallback();
    }
  });
});

// A panel mounted on one configuration, holding one talkgroup: the type the
// configuration carries beside the label is what decides the form the panel
// names the talkgroup by.

function mountedTalkgroup(talkgroup, system = { id: 40, label: 'Metro' }) {
  const { store } = getAppContext();

  store.setState({ categories: [], config: { systems: [{ ...system, talkgroups: [talkgroup] }] }, map: {} });

  const el = document.createElement('rdio-select-panel');

  el.connectedCallback();

  return el;
}

describe('presentation/listener/rdio-select-panel — the talkgroup a type numbers in agency, fleet and subfleet', () => {
  it('names an edacs talkgroup the configuration named with its own reference by the form its type calls for', () => {
    const el = mountedTalkgroup({ id: 1457, label: '1457', type: 'edacs' });

    try {
      const button = talkgroupButton(el, 1457);

      assert.ok(button, 'the panel dropped the talkgroup button');
      assert.equal(button.textContent, '11-061',
        'the button names the talkgroup by the decimal id its type numbers it in');
      assert.equal(button.dataset.talkgroup, '1457',
        'the button no longer carries the reference the panel acts on');
      assert.equal(button.dataset.system, '40',
        'the button no longer carries the system the talkgroup belongs to');
    } finally {
      el.disconnectedCallback();
    }
  });

  it('names it that way when only the system carries the type', () => {
    const el = mountedTalkgroup({ id: 1457, label: '1457' }, { id: 41, label: 'Metro', type: 'edacs' });

    try {
      assert.equal(talkgroupButton(el, 1457)?.textContent, '11-061',
        'the button ignores the type the system the talkgroup sits under carries');
    } finally {
      el.disconnectedCallback();
    }
  });

  it('names a talkgroup an operator named with the name the operator wrote', () => {
    const el = mountedTalkgroup({ id: 1457, label: 'Dispatch 1', type: 'edacs' });

    try {
      const button = talkgroupButton(el, 1457);

      assert.equal(button?.textContent, 'Dispatch 1',
        'the button replaced the label the operator wrote with the form the type calls for');
      assert.equal(button?.dataset.talkgroup, '1457',
        'the button no longer carries the reference the label stands for');
    } finally {
      el.disconnectedCallback();
    }
  });

  it('keeps the decimal id for a talkgroup the rule does not cover', () => {
    const el = mountedTalkgroup({ id: 1457, label: '1457', type: 'smartnet' });

    try {
      assert.equal(talkgroupButton(el, 1457)?.textContent, '1457',
        'the button wrote an agency-fleet-subfleet form for a type that has none');
    } finally {
      el.disconnectedCallback();
    }
  });
});
