// The retired options: the options section
// shows no control for the 12-hour time format, the dimmer delay, the duplicate
// detection time frame, or playback-goes-live, and the working object the section
// edits keeps only the editable keys.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';
import '../presentation/admin/rdio-admin-options.js';

import { findAll } from './dom-shim.js';

const WORKING = {
  audioConversion: 1,
  autoPopulate: true,
  branding: 'Capture Dispatch',
  disableDuplicateDetection: false,
  email: 'support@example.com',
  keypadBeeps: 'uniden',
  maxClients: 50,
  pruneDays: 30,
  showListenersCount: true,
  sortTalkgroups: false,
};

function optionsSection() {
  const { store } = getAdminContext();

  store.setState({
    'section:options': { baseline: null, working: { ...WORKING }, dirty: false, invalid: false, conflict: false },
  });

  const el = new (customElements.get('rdio-admin-options'))();

  el.connectedCallback();

  return el;
}

function fieldTitles(el) {
  return findAll(el, (node) => node.className === 'field-title').map((node) => node.textContent);
}

test('the retired options have no control in the options section', () => {
  const titles = fieldTitles(optionsSection());

  for (const retired of [
    '12-Hour Time Format',
    'Dimmer Delay',
    'Duplicate Call Detection Time Frame',
    'Playback Mode Goes Live',
  ]) {
    assert.ok(!titles.includes(retired), `the section still shows the ${retired} control`);
  }

  for (const kept of ['Max Clients', 'Prune Days', 'Branding Label', 'Keypad Beep Style']) {
    assert.ok(titles.includes(kept), `the section lost the ${kept} control`);
  }
});
