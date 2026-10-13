// The status light: the colour an administrator configured at a group, a tag, a system, or a
// talkgroup is the colour the light shows while that call plays, green when the call carries none,
// and the light keeps its colour while the feed is paused. The rule that picks the colour is
// domain/alert-priority; these cases are the component that applies it.

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
  it('shows a group colour, which wins over every other level', () => {
    assert.deepEqual(
      classes({
        groupsData: [{ label: 'Fire', led: 'red' }],
        tagData: { label: 'T1', led: 'blue' },
        systemData: { label: 'SYS1', led: 'cyan' },
        talkgroupData: { label: 'TG1', led: 'orange' },
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
        talkgroupData: { label: 'TG1', led: 'orange' },
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
        talkgroupData: { label: 'TG1', led: 'orange' },
      }),
      ['led', 'on', 'cyan'],
    );
  });

  it('shows a talkgroup colour when nothing above it carries one', () => {
    assert.deepEqual(
      classes({
        groupsData: [{ label: 'Fire' }],
        tagData: { label: 'T1' },
        systemData: { label: 'SYS1' },
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
        systemData: { label: 'SYS1' },
        talkgroupData: { label: 'TG1' },
      }),
      ['led', 'on', 'green'],
    );
  });

  it('shows green for a colour the eight the light knows do not include', () => {
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
