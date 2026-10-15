// The login form tells the truth about why it failed: a wrong
// password says Invalid password; a server that cannot be reached is named as
// unreachable, not as a bad password.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

// The form field primitive is part of every bundle the administration application builds; a
// test that reads a field's message needs it defined before the form markup is parsed.
import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-login.js';

import { connectFields, fieldMessage, findAll } from './dom-shim.js';

// The form markup arrives through innerHTML, the way a browser inserts it: the fields under the
// root are connected here, which is the moment the browser runs the primitive's own setup.
function loginForm(result) {
  const context = getAdminContext();

  context.useCases.login = async () => result;

  const el = new (customElements.get('rdio-admin-login'))();

  el.connectedCallback();
  connectFields(el);

  const form = el.querySelector('form');
  const input = el.querySelector('input[name="password"]');

  // The operator types: the value lands in the control and the input event bubbles out of it,
  // which is what makes the field one the operator has reached.
  input.value = 'secret';
  input.dispatchEvent({ type: 'input' });

  form.dispatchEvent({ type: 'submit', preventDefault: () => {} });

  return new Promise((resolve) => {
    setImmediate(() => resolve(el));
  });
}

// The message is the text under the control, written by the form field primitive itself.
function errorText(el) {
  return fieldMessage(el.querySelector('rdio-form-field'));
}

// The browser filled the password in: the value is in the control and no event ever left it, so
// the field is one the operator has not reached by typing. Pressing Enter submits it.
function pastedForm(result, password) {
  const context = getAdminContext();

  context.useCases.login = async () => result;

  const el = new (customElements.get('rdio-admin-login'))();

  el.connectedCallback();
  connectFields(el);

  const input = el.querySelector('input[name="password"]');

  input.value = password;

  el.querySelector('form').dispatchEvent({ type: 'submit', preventDefault: () => {} });

  return new Promise((resolve) => {
    setImmediate(() => resolve(el));
  });
}

test('the login form carries the form field primitive, not a node that only records a message', () => {
  const el = new (customElements.get('rdio-admin-login'))();

  el.connectedCallback();
  connectFields(el);

  const field = el.querySelector('rdio-form-field');

  assert.equal(field.tag, 'rdio-form-field');
  assert.equal(field.dataset.bound, '1');
  assert.equal(typeof field.isReached, 'function');
  assert.equal(fieldMessage(field), '');
});

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

test('a submit that never left the field tells the operator the code was refused', async () => {
  const el = await pastedForm('invalid', 'hunter2');

  assert.equal(errorText(el), 'Invalid password', 'submitting the form reaches the field it submits');
});

test('the password field declares the purpose the browser fills it for', async () => {
  const el = await loginForm('ok');

  const input = el.querySelector('input[name="password"]');

  assert.equal(input.getAttribute('autocomplete'), 'current-password',
    'the login field names its purpose so the password manager and the browser fill it right');
});
