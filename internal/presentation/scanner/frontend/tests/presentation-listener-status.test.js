// the status light: the colour the server resolved for a call shows while that call
// plays, green when nothing was configured, and the light keeps its colour while the
// feed is paused. The server names a colour on every system and talkgroup entry it
// sends, and folds the system colour into the talkgroup entry. domain/alert-priority
// picks the colour from what the call carries.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { getAppContext } from '../application/context.js';
import '../presentation/listener/rdio-status.js';

function shell() {
  const { store } = getAppContext();

  const el = document.createElement('rdio-status');

  el.connectedCallback();

  return { store, el, led: () => el.querySelector('.led') };
}

function classes(call, { pause = false } = {}) {
  const { store, el, led } = shell();

  store.setState({ call, pause });

  return led().className.split(/\s+/);
}

describe('presentation/listener/rdio-status — the colour a call carries', () => {
  // Each call below is a call the server has resolved: an administrator stored a colour at
  // one or more levels, and the talkgroup entry carries the colour that came out on top.
  it('shows a group colour, which wins over every other level', () => {
    assert.deepEqual(
      classes({
        groupsData: [{ label: 'Fire', led: 'red' }],
        tagData: { label: 'T1', led: 'blue' },
        systemData: { label: 'SYS1', led: 'cyan' },
        talkgroupData: { label: 'TG1', led: 'red' },
      }),
      ['led', 'on', 'red'],
    );
  });

  it('shows a tag colour when no group carries one', () => {
    assert.deepEqual(
      classes({
        groupsData: [{ label: 'Fire' }],
        tagData: { label: 'T1', led: 'blue' },
        systemData: { label: 'SYS1', led: 'cyan' },
        talkgroupData: { label: 'TG1', led: 'blue' },
      }),
      ['led', 'on', 'blue'],
    );
  });

  it('shows a system colour when no tag carries one', () => {
    assert.deepEqual(
      classes({
        groupsData: [{ label: 'Fire' }],
        tagData: { label: 'T1' },
        systemData: { label: 'SYS1', led: 'cyan' },
        talkgroupData: { label: 'TG1', led: 'cyan' },
      }),
      ['led', 'on', 'cyan'],
    );
  });

  it('shows a talkgroup colour when nothing above it carries one', () => {
    assert.deepEqual(
      classes({
        groupsData: [{ label: 'Fire' }],
        tagData: { label: 'T1' },
        systemData: { label: 'SYS1', led: 'green' },
        talkgroupData: { label: 'TG1', led: 'orange' },
      }),
      ['led', 'on', 'orange'],
    );
  });

  it('shows green for a call carrying no colour', () => {
    assert.deepEqual(
      classes({
        groupsData: [{ label: 'Fire' }],
        tagData: { label: 'T1' },
        systemData: { label: 'SYS1', led: 'green' },
        talkgroupData: { label: 'TG1', led: 'green' },
      }),
      ['led', 'on', 'green'],
    );
  });

  it('shows green for a colour the eight the light knows do not include', () => {
    // A group entry carries what the administrator stored, so a name outside the eight
    // reaches the light verbatim; the light still answers green.
    assert.deepEqual(classes({ groupsData: [{ label: 'Fire', led: 'pink' }] }), ['led', 'on', 'green']);
    assert.deepEqual(classes({ talkgroupData: { label: 'TG1', led: 'pink' } }), ['led', 'on', 'green']);
  });

  it('keeps the colour while the feed is paused', () => {
    assert.deepEqual(
      classes({ groupsData: [{ label: 'Fire', led: 'red' }] }, { pause: true }),
      ['led', 'on', 'paused', 'red'],
    );
  });

  it('shows green and the off state with no call playing', () => {
    assert.deepEqual(classes(null), ['led', 'off', 'green']);
  });

  it('names the light for a screen reader after the state it shows', () => {
    const { store, led } = shell();

    store.setState({ call: { groupsData: [{ label: 'Fire', led: 'red' }] }, pause: false });

    assert.equal(led().getAttribute('aria-label'), 'Server status: on');
  });
});
