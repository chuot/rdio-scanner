// The todos banner (T013, CA-001, CA-002): todos are shown in a persistent banner at
// the top of the screen, not as a section. The banner renders nothing when there is
// nothing to say, and the navigation no longer carries a todos item.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';
import '../presentation/admin/rdio-admin.js';

import { findAll, texts } from './dom-shim.js';

function shell() {
  const { store } = getAdminContext();

  store.setState({ auth: { authenticated: true, docker: false, passwordNeedChange: false } });

  const el = new (customElements.get('rdio-admin'))();

  el.connectedCallback();

  return { store, el };
}

test('the banner shows the open todos in the content column, above the section card', () => {
  const { store, el } = shell();

  store.setState({
    todos: [
      { level: 'warn', message: 'the admin password must be changed' },
      { level: 'info', message: 'a talkgroup has no group' },
    ],
  });

  const banners = findAll(el, (node) => node.className.includes('todos-banner'));

  assert.equal(banners.length, 1, 'the shell does not show a todos banner');

  const items = findAll(banners[0], (node) => node.className.startsWith('todo '));

  assert.equal(items.length, 2, 'every open todo must be a row of the banner');

  const labels = findAll(banners[0], (node) => node.tag === 'span');

  assert.deepEqual(texts(labels), [
    'the admin password must be changed',
    'a talkgroup has no group',
  ]);

  const contents = findAll(el, (node) => node.className.includes('rdio-admin-content'));

  assert.equal(contents.length, 1, 'the shell has one content column');
  assert.equal(contents[0].children[0], banners[0],
    'the banner must sit in the content column, above the section card');
});

test('the banner renders nothing when there is nothing to say', () => {
  const { store, el } = shell();

  store.setState({ todos: [] });

  const banners = findAll(el, (node) => node.className.includes('todos-banner'));

  assert.equal(banners.length, 0,
    'an empty todo list must leave no banner in the content column');
});

test('todos is not a section in the navigation', () => {
  const { el } = shell();

  const navs = findAll(el, (node) => node.getAttribute('aria-label') === 'Admin sections');

  assert.equal(navs.length, 1);

  const sections = navs[0].children.map((button) => button.dataset.section);

  assert.ok(!sections.includes('todos'), 'the navigation still carries a todos item');
  assert.equal(sections[0], 'access', 'the first section is the one the page opens on');
});
