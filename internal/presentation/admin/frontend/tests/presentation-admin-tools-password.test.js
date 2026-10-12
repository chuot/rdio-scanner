// The password panel in Tools: the form lives inside the Tools
// section as a details panel, carries no username field, and the standalone
// password section is gone from the navigation.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-tools.js';

import { findAll, texts } from './dom-shim.js';

function toolsSection() {
  const { store } = getAdminContext();

  store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    'section:systems': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:groups': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:tags': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:access': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:apikeys': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:dirwatch': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:downstreams': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:options': { baseline: null, working: null, dirty: false, invalid: false, conflict: false },
    alerts: {},
  });

  const el = new (customElements.get('rdio-admin-tools'))();

  el.connectedCallback();

  return el;
}

function panelTitles(el) {
  return texts(findAll(el, (node) => node.className === 'row-title'));
}

test('Tools carries the Change password panel', () => {
  const titles = panelTitles(toolsSection());

  assert.ok(titles.includes('Change password'), 'Tools has no Change password panel');
});

test('the password panel has no username field and the three password fields', () => {
  const el = toolsSection();

  assert.equal(findAll(el, (node) => node.attributes.name === 'username').length, 0, 'the panel still shows the username field');

  for (const name of ['currentPassword', 'newPassword', 'verifyNewPassword']) {
    const inputs = findAll(el, (node) => node.attributes.name === name);

    assert.equal(inputs.length, 1, `the panel has no ${name} field`);
    assert.equal(inputs[0].attributes.type, 'password', `the ${name} field is not a password input`);
  }
});

test('the password panel Save starts disabled and the form validates live', () => {
  const el = toolsSection();

  const save = findAll(el, (node) => node.tag === 'button' && node.className.includes('save'))[0];

  assert.ok(save, 'the panel has no Save button');
  assert.equal(save.disabled, true, 'Save must start disabled');

  const current = findAll(el, (node) => node.attributes.name === 'currentPassword')[0];
  const fresh = findAll(el, (node) => node.attributes.name === 'newPassword')[0];
  const verify = findAll(el, (node) => node.attributes.name === 'verifyNewPassword')[0];

  current.value = 'old-secret';
  fresh.value = 'new-secret-long';
  verify.value = 'new-secret-long';

  for (const input of [current, fresh, verify]) {
    for (const fn of input.listeners.input ?? []) fn();
  }

  assert.equal(save.disabled, false, 'Save must enable when the form is valid');

  verify.value = 'different';

  for (const fn of verify.listeners.input ?? []) fn();

  assert.equal(save.disabled, true, 'Save must disable on a password mismatch');
});
