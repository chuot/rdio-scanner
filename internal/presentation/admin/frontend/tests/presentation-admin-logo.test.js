// The Rdio Scanner logo in the top bar and on the login card (T014, CA-003): the
// bundled PWA icon plus the wordmark, on both surfaces the administrator sees.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';
import '../presentation/admin/rdio-admin.js';
import '../presentation/admin/rdio-admin-login.js';

import { findAll } from './dom-shim.js';

const LOGO_SRC = 'assets/icons/icon-192x192.png';

function authenticatedShell() {
  const { store } = getAdminContext();

  store.setState({ auth: { authenticated: true, docker: false, passwordNeedChange: false } });

  const el = new (customElements.get('rdio-admin'))();

  el.connectedCallback();

  return el;
}

function loginCard() {
  const { store } = getAdminContext();

  store.setState({ auth: { authenticated: false, docker: false, passwordNeedChange: false } });

  const el = new (customElements.get('rdio-admin-login'))();

  el.connectedCallback();

  return el;
}

function assertLogo(root, where) {
  const imgs = findAll(root, (node) => node.tag === 'img');

  assert.equal(imgs.length, 1, `the ${where} does not show the logo`);
  assert.equal(imgs[0].getAttribute('src'), LOGO_SRC, `the ${where} logo is not the bundled icon`);
  assert.equal(imgs[0].getAttribute('alt'), 'Rdio Scanner', `the ${where} logo has no wordmark alt`);

  const titles = findAll(root, (node) => node.tag === 'h1');

  assert.ok(titles.length > 0, `the ${where} has no title`);
  assert.ok(titles[0].textContent.includes('Rdio Scanner'), `the ${where} title is not the wordmark`);
}

test('the top bar shows the logo and the wordmark', () => {
  const el = authenticatedShell();

  assertLogo(el.children[0], 'top bar');
});

test('the login card shows the logo and the wordmark', () => {
  const el = loginCard();

  assertLogo(el, 'login card');
});
