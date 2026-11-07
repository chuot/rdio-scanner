// the listener says what the screen shows: one polite region the page
// writes into with the words the contract fixes, when a state the
// display or the search panel owns changes.

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import './dom-shim.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/listener/rdio-app.js';
import '../presentation/listener/rdio-controls.js';
import '../presentation/listener/rdio-display.js';
import '../presentation/listener/rdio-search-panel.js';
import '../presentation/listener/rdio-select-panel.js';
import '../presentation/listener/rdio-status.js';

import { findAll } from './dom-shim.js';

import { getAppContext } from '../application/context.js';

const CONFIG = {
  email: '',
  groups: { Dispatch: {}, Interop: {} },
  tags: { Fire: {} },
  systems: [{
    id: 1,
    label: 'Metro',
    talkgroups: [
      { id: 10, label: 'Fire dispatch', groups: ['Dispatch'], tag: 'Fire' },
      { id: 11, label: 'Tac dispatch', groups: ['Interop'], tag: 'Fire' },
    ],
  }],
};

const CALL = {
  id: 7,
  system: 1,
  talkgroup: 10,
  talkgroupData: { label: 'Fire dispatch', name: 'Fire dispatch' },
  systemData: { label: 'Metro' },
  frequency: 155000,
  dateTime: Date.now(),
};

// the shell, rendered the way the page renders it: the shim connects
// every element the shell's markup carries. the promo and display
// timers are handed to the test during the render and left unrun.
function shell() {
  document.body.replaceChildren();

  getAppContext().store.setState({
    ...getAppContext().store.getState(),
    config: CONFIG,
  });

  // the search the panel asks for is the network's answer; the shim has
  // no network, so the answer is what the test hands the store.
  getAppContext().useCases.searchCalls = () => {};

  const el = document.createElement('rdio-app');

  const realSetTimeout = globalThis.setTimeout;
  const realSetInterval = globalThis.setInterval;

  globalThis.setTimeout = () => 0;
  globalThis.setInterval = () => 0;

  try {
    el.connectedCallback();
  } finally {
    globalThis.setTimeout = realSetTimeout;
    globalThis.setInterval = realSetInterval;
  }

  document.body.append(el);

  return el;
}

function region(el) {
  return findAll(el, (node) => node.tag === 'rdio-live-region')[0] ?? null;
}

function said(el) {
  return region(el)?.textContent.trim() ?? '';
}

function searchPanel(el) {
  return findAll(el, (node) => node.tag === 'rdio-search-panel')[0];
}

describe('listener announcements — the page says what the screen shows', () => {
  it('carries a polite live region, empty of anything the page shows all the time', () => {
    const el = shell();
    const live = region(el);

    assert.ok(live, 'the listener carries no live region for the operator who cannot see the screen');
    assert.equal(live.getAttribute('aria-live'), 'polite', 'the region interrupts the operator');
    assert.equal(live.getAttribute('role'), 'status');
    assert.equal(said(el), '', `the region carries static content: ${said(el)}`);
  });

  it('names the talkgroup the listener starts playing', () => {
    const el = shell();

    getAppContext().store.setState({ ...getAppContext().store.getState(), call: CALL });

    assert.equal(said(el), 'Playing Fire dispatch', 'the page did not say the talkgroup it started playing');
  });

  it('names the talkgroup the listener stopped playing', () => {
    const el = shell();

    getAppContext().store.setState({ ...getAppContext().store.getState(), call: CALL });
    getAppContext().store.setState({ ...getAppContext().store.getState(), call: null });

    assert.equal(said(el), 'Stopped Fire dispatch', 'the page did not say the playback ended');
  });

  it('names the talkgroup the listener paused', () => {
    const el = shell();

    getAppContext().store.setState({ ...getAppContext().store.getState(), call: CALL });
    getAppContext().store.setState({ ...getAppContext().store.getState(), call: CALL, pause: true });

    assert.equal(said(el), 'Paused Fire dispatch', 'the page did not say the playback paused');
  });

  it('says the queue as it fills', () => {
    const el = shell();

    getAppContext().store.setState({ ...getAppContext().store.getState(), queue: 3 });

    assert.equal(said(el), 'Queue: 3', 'the page did not say the queue the listener is holding');
  });

  it('says how many calls the search answered with', () => {
    const el = shell();

    searchPanel(el).open();

    getAppContext().store.setState({
      ...getAppContext().store.getState(),
      playbackList: { count: 42, options: { offset: 0 }, results: [{ id: 1, dateTime: Date.now() }] },
      playbackListSeq: 1,
    });

    assert.equal(said(el), '42 calls', 'the page did not say how many calls the search answered with');
  });

  it('says the page the list moved to', () => {
    const el = shell();

    getAppContext().store.setState({
      ...getAppContext().store.getState(),
      playbackList: { count: 500, options: { offset: 0 }, results: [{ id: 1, dateTime: Date.now() }] },
      playbackListSeq: 1,
    });

    const paginator = findAll(searchPanel(el), (node) => node.tag === 'rdio-paginator')[0];

    const next = findAll(paginator, (node) => node.tag === 'button' && node.getAttribute('aria-label') === 'Next page')[0];

    assert.ok(next, 'the list offers no control the operator uses to turn the page');

    next.click();

    assert.equal(said(el), 'Page 2', 'the page did not say the page the list moved to');
  });
});
