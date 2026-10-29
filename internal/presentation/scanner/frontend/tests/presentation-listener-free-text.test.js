// the free text three listener surfaces write as markup: the search
// panel's results table, the display's history table, and the help
// snackbar's configured email. each interpolates text the server
// carries into an innerHTML template, and none of them builds that
// text as text. systems and talkgroups are named by free text an
// operator saves, the ingest path can plant those labels from call
// metadata a weaker credential than an admin login supplies, and the
// contact email is stored verbatim: all three reach a listener page
// verbatim in the configuration and the call answers the server sends.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { afterEach, beforeEach, describe, it } from 'node:test';

import { getAppContext } from '../application/context.js';

import { dismissSnackbar } from '../../../common/frontend/presentation/ui/primitives.js';
import { showHelp as showListenerHelp } from '../presentation/listener/rdio-help.js';

import '../presentation/listener/rdio-display.js';
import '../presentation/listener/rdio-search-panel.js';

import { connectFields, findAll } from './dom-shim.js';

// Anything the free text built rather than the component's own markup:
// an element the text introduced, or an attribute it introduced on one.
// The same reading the select panel's cases take.
const builtFromTheText = (root) =>
  findAll(root, (n) => n.tag === 'img' || n.tag === 'script' || n.attributes.onerror !== undefined)
    .map((n) => (n.attributes.onerror !== undefined ? `${n.tag}[onerror]` : n.tag));

// A label carrying an element and a quote that closes the attribute
// it sits in: the quote is what turns a cell's own title into a place
// the text can add attributes to.
const MARKUP = 'Fire "><script>alert(1)</script><img src="x" onerror="arm()"> Station';

// ---------------------------------------------------------------------------
// the search panel's results table

function answeredPanel(rows) {
  const { store } = getAppContext();

  store.setState({
    config: { email: '', groups: {}, tags: {}, systems: [] },
    categories: [],
    map: {},
    playbackList: undefined,
    playbackListSeq: 0,
    call: undefined,
    livefeedMode: 'offline',
  });

  const el = document.createElement('rdio-search-panel');

  el.connectedCallback();

  getAppContext().store.setState({
    playbackList: { count: rows.length, options: { limit: 200, offset: 0 }, results: rows },
    playbackListSeq: 1,
  });

  return el;
}

const searchRow = (id) => ({
  id,
  system: 1,
  talkgroup: 11,
  groups: [],
  tag: '',
  dateTime: new Date(2026, 9, 6, 10, 30).toISOString(),
  audioLength: 30,
  systemData: { label: MARKUP },
  talkgroupData: { label: MARKUP, name: MARKUP },
});

describe('presentation/listener/rdio-search-panel — labels the answer carries', () => {
  it('names a row by labels that carry markup, building no element out of those labels', () => {
    const el = answeredPanel([searchRow(101)]);

    try {
      const spans = findAll(el, (n) => n.tag === 'span');

      assert.ok(spans.some((n) => n.textContent.includes('Station')),
        'the results table dropped the label the answer carried');

      assert.deepEqual(builtFromTheText(el), [],
        'a saved label built an element or an attribute of its own into the results table');
    } finally {
      el.disconnectedCallback();
    }
  });
});

// ---------------------------------------------------------------------------
// the display's history table

// the display records the call it leaves behind into the history, and
// that call lives inside the use-cases: the case names it through the
// same reading the component takes.
const previousSlot = () => {
  const { useCases } = getAppContext();

  const original = Object.getOwnPropertyDescriptor(useCases, 'callPrevious');
  const slot = { call: null };

  Object.defineProperty(useCases, 'callPrevious', {
    configurable: true,
    get: () => slot.call,
  });

  return { slot, restore: () => Object.defineProperty(useCases, 'callPrevious', original) };
};

let display;
let previous;

beforeEach(() => {
  previous = previousSlot();

  display = document.createElement('rdio-display');

  display.connectedCallback();
  connectFields(display);

  getAppContext().store.setState({
    auth: { status: 'anonymous', denied: false },
    call: undefined,
  });
});

afterEach(() => {
  display.disconnectedCallback();
  previous.restore();
});

describe('presentation/listener/rdio-display — labels the history carries', () => {
  it('keeps a history row out of the text it truncates, building no element out of that text', () => {
    const { store } = getAppContext();

    const poisoned = {
      id: 1,
      system: 10,
      talkgroup: 100,
      frequency: 855e6,
      dateTime: 1721692800000,
      audio: { data: [1] },
      systemData: { label: MARKUP },
      talkgroupData: { label: MARKUP, name: MARKUP },
    };

    previous.slot.call = poisoned;

    store.setState({ call: { id: 2, name: 'Car 1', audio: { data: [1] } } });

    const history = findAll(display, (n) => n.tag === 'tr' && n.parent?.tag === 'tbody')[0];

    assert.ok(history, 'the history has no first row to carry the entry');

    const titles = findAll(history, (n) => n.tag === 'td').map((td) => td.attributes.title);

    assert.ok(titles.some((title) => title?.includes('Station')),
      'the history row does not carry the full text of the label on the cell that clips it');

    assert.deepEqual(builtFromTheText(history), [],
      'a history label built an element or an attribute of its own into the history row');
  });
});

// ---------------------------------------------------------------------------
// the help snackbar's configured email

describe('presentation/listener/rdio-help — the configured contact email', () => {
  let timers;
  let realSetInterval;
  let realSetTimeout;

  beforeEach(() => {
    document.body.replaceChildren();

    timers = [];
    realSetInterval = globalThis.setInterval;
    realSetTimeout = globalThis.setTimeout;

    globalThis.setInterval = (fn, ms) => {
      timers.push(fn);

      return timers.length;
    };
    globalThis.setTimeout = (fn, ms) => {
      timers.push(fn);

      return timers.length;
    };
  });

  afterEach(() => {
    globalThis.setInterval = realSetInterval;
    globalThis.setTimeout = realSetTimeout;

    dismissSnackbar();
    document.body.replaceChildren();
  });

  it('links an email that carries markup without building an element out of it', () => {
    const email = '"><script>alert(1)</script>dispatch@example.com';

    showListenerHelp(email);

    const link = findAll(document.body, (n) => n.tag === 'a')[0];

    assert.ok(link, 'the help message carries no support link');
    assert.ok(link.textContent.includes('dispatch@example.com'),
      'the help message does not name the configured email');

    assert.deepEqual(builtFromTheText(document.body), [],
      'the configured email built an element or an attribute of its own into the help message');
  });
});
