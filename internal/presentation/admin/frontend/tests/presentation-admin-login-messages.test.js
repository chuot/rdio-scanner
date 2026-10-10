// The login form tells the truth about why it failed (FEAT-024, CA-008): a wrong
// password says Invalid password; a server that cannot be reached is named as
// unreachable, not as a bad password.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-login.js';

function loginForm(result) {
  const context = getAdminContext();

  context.useCases.login = async () => result;

  const el = new (customElements.get('rdio-admin-login'))();

  el.connectedCallback();

  const form = el.querySelector('form');
  const input = el.querySelector('input[name="password"]');

  input.value = 'secret';

  form.dispatchEvent({ type: 'submit', preventDefault: () => {} });

  return new Promise((resolve) => {
    setImmediate(() => resolve(el));
  });
}

function errorText(el) {
  return el.querySelector('rdio-form-field').error;
}

test('a wrong password shows Invalid password', async () => {
  const el = await loginForm('invalid');

  assert.equal(errorText(el), 'Invalid password');
});

test('an unreachable server is named as unreachable, not as a bad password', async () => {
  const el = await loginForm('unreachable');

  assert.equal(errorText(el), 'The server cannot be reached. Check that it is running.');
});

test('a successful login shows no error', async () => {
  const el = await loginForm('ok');

  assert.equal(errorText(el), '');
});
