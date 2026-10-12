// The todos banner: todos are shown in a persistent banner at
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

test('the topbar carries the brand and the logout control, and no role text', () => {
  const { el } = shell();

  const roles = findAll(el, (node) => node.className.includes('rdio-admin-role'));

  assert.equal(roles.length, 0, 'the topbar must not show the Admin role text');

  const headers = findAll(el, (node) => node.className.includes('rdio-admin-header'));

  assert.equal(headers.length, 1, 'the shell has one header');
  assert.ok(texts(findAll(headers[0], (node) => node.tag === 'h1')).includes('Rdio Scanner'), 'the header must carry the brand');
});

test('the navigation lists the sections alphabetically by label', () => {
  const { el } = shell();

  const navs = findAll(el, (node) => node.className.includes('rdio-admin-nav'));

  assert.equal(navs.length, 1, 'the shell has one navigation column');

  const labels = texts(findAll(navs[0], (node) => node.tag === 'span'));

  assert.deepEqual(labels, [...labels].sort((a, b) => a.localeCompare(b, 'en')), 'the navigation is not alphabetical by label');

  assert.ok(!labels.includes('Ingestion'), 'the navigation still carries the Ingestion section');
  assert.ok(labels.includes('OpenAPI'), 'the navigation does not carry the OpenAPI section');
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

test('a todo that names the control clearing it is a control the operator can follow', () => {
  const { store, el } = shell();

  store.setState({
    todos: [{
      level: 'info',
      message: 'No systems defined. Define one from the Systems menu, or turn on Options ▸ Auto Populate to have them created for you.',
      step: { section: 'options', control: 'autoPopulate' },
    }],
  });

  const steps = findAll(el, (node) => node.className === 'todo-step');

  assert.equal(steps.length, 1, 'the todo that names a control must be followable');
  assert.ok(texts(findAll(steps[0], (node) => node.tag === 'span'))
    .includes('No systems defined. Define one from the Systems menu, or turn on Options ▸ Auto Populate to have them created for you.'),
  'the followable todo must state the path it follows');

  const plain = findAll(el, (node) => node.className.startsWith('todo ') && !findAll(node, (n) => n.className === 'todo-step').length);

  assert.equal(plain.length, 0, 'the stepped todo must not also render as a plain row');
});

test('following the todo opens the section that owns the control and asks for its attention', () => {
  const { store, el } = shell();

  store.setState({
    todos: [{ level: 'info', message: 'no systems', step: { section: 'options', control: 'autoPopulate' } }],
  });

  findAll(el, (node) => node.className === 'todo-step')[0].click();

  assert.deepEqual(store.getState().focus, { section: 'options', control: 'autoPopulate' },
    'following the todo must raise the request the section consumes');

  const opened = findAll(el, (node) => node.tag === 'rdio-admin-options');

  assert.equal(opened.length, 1, 'following the todo must open the Options section');
});
