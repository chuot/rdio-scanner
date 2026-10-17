// The section a signed-in operator lands on (presentation/admin/rdio-admin.js). The landing is what the
// operator sees before they choose anything, so it is proven through the two ways a session really starts:
// a token the browser already holds, and a password the operator just typed. Both go through the shell's
// own context, its own adapter, and a transport the test answers, so the reading is taken on the page the
// operator is actually on and not on a stub that agrees with the code under test.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';
import { ADMIN_TOKEN_KEY } from '../infrastructure/token-storage.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin.js';

import { findAll } from './dom-shim.js';

// The transport the shell's context is built on is answered here, because the context is built the first
// time a component asks for it and the adapter keeps whatever fetch the page had at that moment.
const requests = [];

const CONFIG = {
  access: [],
  apikeys: [],
  dirwatch: [],
  downstreams: [],
  groups: [],
  options: { branding: 'shed' },
  systems: [],
  tags: [],
};

const STATUS = {
  server: {
    version: '8.0.0',
    docker: false,
    startedAt: '2026-10-17T06:31:03Z',
    now: '2026-10-17T08:45:11Z',
    uptimeSeconds: 8048,
    baseDir: '/data/rdio',
    databaseBytes: 4831838208,
    databaseKnown: true,
  },
  live: { listeners: 3, administratorSessions: 1, dirwatchersRunning: 0, heldCalls: 0, audioConversion: true, audioProbe: true },
  activity: {
    windowStart: '2026-10-16T08:00:00Z',
    windowEnd: '2026-10-17T08:00:00Z',
    callsToday: 214,
    callsLastHour: 37,
    lastCallAt: '2026-10-17T08:44:02Z',
    hours: [],
    busiest: [],
    recent: [],
  },
  logs: { windowStart: '2026-10-16T08:45:11Z', errors: 0, warnings: 0, sources: [] },
};

globalThis.WebSocket = class FakeAdminSocket {
  constructor(url) {
    this.url = url;
    this.sent = [];
  }

  send(message) {
    this.sent.push(message);
  }

  close() {
    this.onclose?.({ code: 1000 });
  }
};

globalThis.fetch = async (url, init = {}) => {
  requests.push({ url: String(url), method: init.method ?? 'GET', headers: init.headers ?? {} });

  if (url.endsWith('/api/admin/login')) return answer(200, { token: 'jwt-for-the-landing', passwordNeedChange: false });
  if (url.endsWith('/api/admin/logout')) return answer(200, undefined);
  if (url.endsWith('/api/admin/config')) return answer(200, { docker: false, passwordNeedChange: false, config: CONFIG });
  if (url.endsWith('/api/admin/alerts')) return answer(200, {});
  if (url.endsWith('/api/admin/status')) return answer(200, STATUS);

  return answer(404, undefined);
};

function answer(status, body) {
  return {
    status,
    ok: status >= 200 && status < 300,
    async json() {
      if (body === undefined) throw new Error('no body');

      return structuredClone(body);
    },
  };
}

// The shell, opened the way the browser opens it: connected to a page, with the session the browser holds.
function openShell() {
  const el = new (customElements.get('rdio-admin'))();

  el.connectedCallback();

  return el;
}

// The session the page holds, taken away: the store is the page's own, and a page that has signed out is a
// page whose store says so.
function signOut() {
  window.sessionStorage.clear();

  getAdminContext().store.setState({ auth: { authenticated: false, docker: false, passwordNeedChange: false } });
}

function contentRegion(el) {
  return findAll(el, (node) => node.tag === 'section' && node.className?.includes('rdio-admin-content'))[0] ?? null;
}

function sectionOnPage(el) {
  const region = contentRegion(el);

  return region?.children.find((node) => node && typeof node === 'object' && node.tag?.startsWith('rdio-admin-')) ?? null;
}

function headingOf(el) {
  const region = contentRegion(el);
  const headingId = region?.getAttribute('aria-labelledby');

  return findAll(el, (node) => /^h[1-6]$/.test(node.tag)).find((node) => node.getAttribute('id') === headingId) ?? null;
}

function navButtons(el) {
  const nav = findAll(el, (node) => node.tag === 'nav')[0];

  return findAll(nav, (node) => node.tag === 'button' && node.dataset.section !== undefined);
}

// The word the operator reads on a navigation item. The icon beside it is the icon font's own ligature
// text, which the markup hides from the operator who is reading rather than looking, so the label is the
// span and not the whole button.
function navLabel(button) {
  const label = findAll(button, (node) => node.tag === 'span')[0];

  return (label?.textContent ?? button.textContent ?? '').trim();
}

// The landing is the section the operator is shown before they choose anything. Everything the operator
// needs first — what the server is doing, whether it is still ingesting — is on the dashboard, and the
// operator should not have to know that in order to go looking for it.
test('an operator whose browser already holds a session lands on the dashboard', async () => {
  window.sessionStorage.setItem(ADMIN_TOKEN_KEY, 'a-token-the-browser-is-holding');

  const el = openShell();

  await getAdminContext().useCases.boot();

  const landed = sectionOnPage(el);

  assert.equal(landed?.tag, 'rdio-admin-dashboard', `the page landed on <${landed?.tag ?? 'nothing'}>, not on the dashboard`);
  assert.equal(headingOf(el)?.textContent.trim(), 'Dashboard', 'the heading does not name the section the operator landed on');
});

// The other way a session starts: the operator types a password and the server answers. The landing is the
// same, because it is the same shell answering the same question.
test('an operator who has just signed in lands on the dashboard', async () => {
  signOut();

  const el = openShell();

  const result = await getAdminContext().useCases.login('the-operator-typed-this');

  assert.equal(result, 'ok', 'the sign-in did not succeed on this fixture');

  const landed = sectionOnPage(el);

  assert.equal(landed?.tag, 'rdio-admin-dashboard', `the page landed on <${landed?.tag ?? 'nothing'}>, not on the dashboard`);
  assert.equal(headingOf(el)?.textContent.trim(), 'Dashboard');
});

// An operator who is not signed in is shown the sign-in page and nothing else: the dashboard reads the
// status of a server, and a server that has not let this browser in has no reason to describe itself.
test('an operator who is not signed in is not shown the dashboard', () => {
  signOut();

  const asked = requests.length;

  const el = openShell();

  assert.equal(sectionOnPage(el)?.tag, undefined, 'the page put a section on the sign-in page');

  assert.ok(findAll(el, (node) => node.tag === 'rdio-admin-login').length, 'the page does not carry the sign-in panel');

  // A page that has not been let in asks for nothing: the status read is several statements over the
  // history, and an anonymous page could ask for it as easily as anyone else.
  assert.equal(requests.length, asked, `the sign-in page asked the server for ${requests.length - asked} thing(s) it has not earned`);
});

// The navigation is the operator's map of the sections, and the landing is on it like every other section:
// the operator who lands on the dashboard has to be able to see that is where they are, and to come back to
// it after visiting somewhere else.
test('the dashboard is in the navigation, in the alphabetical place its label puts it', async () => {
  window.sessionStorage.setItem(ADMIN_TOKEN_KEY, 'a-token-the-browser-is-holding');

  const el = openShell();

  await getAdminContext().useCases.boot();

  const items = navButtons(el);

  const sections = items.map((button) => button.dataset.section);

  assert.ok(sections.includes('dashboard'), `the navigation carries ${sections.join(', ')}, and the dashboard is not among it`);

  const labels = items.map((button) => navLabel(button));

  assert.deepEqual(labels, [...labels].sort((a, b) => a.localeCompare(b)), `the navigation is not in the order its labels sort in: ${labels.join(', ')}`);

  assert.equal(sections.indexOf('dashboard'), sections.indexOf('apikeys') + 1, 'Dashboard does not sit after API Keys');
  assert.equal(sections.indexOf('dirwatch'), sections.indexOf('dashboard') + 1, 'Dashboard does not sit before Dirwatch');
});

// Only the section the operator is standing on is current. The landing is the section they are standing on
// until they choose another, and the navigation has to say so.
test('the landing is the item the navigation marks as current', async () => {
  window.sessionStorage.setItem(ADMIN_TOKEN_KEY, 'a-token-the-browser-is-holding');

  const el = openShell();

  await getAdminContext().useCases.boot();

  const currents = navButtons(el).filter((button) => button.getAttribute('aria-current') !== null);

  assert.equal(currents.length, 1, `the navigation marks ${currents.length} items as current`);
  assert.equal(currents[0].getAttribute('aria-current'), 'page');
  assert.equal(currents[0].dataset.section, 'dashboard', 'the item marked current is not the dashboard the operator landed on');
});

// The landing is a section like every other section: the operator can leave it and come back to it, and
// coming back to it puts the same page on the screen again.
test('the operator can leave the dashboard and come back to it', async () => {
  window.sessionStorage.setItem(ADMIN_TOKEN_KEY, 'a-token-the-browser-is-holding');

  const el = openShell();

  await getAdminContext().useCases.boot();

  el.openSection('logs');

  assert.equal(sectionOnPage(el)?.tag, 'rdio-admin-logs', 'the operator did not land on the logs section');
  assert.equal(headingOf(el)?.textContent.trim(), 'Logs');

  el.openSection('dashboard');

  assert.equal(sectionOnPage(el)?.tag, 'rdio-admin-dashboard', 'the operator could not come back to the dashboard');
  assert.equal(headingOf(el)?.textContent.trim(), 'Dashboard');

  const currents = navButtons(el).filter((button) => button.getAttribute('aria-current') === 'page');

  assert.equal(currents.length, 1);
  assert.equal(currents[0].dataset.section, 'dashboard', 'the navigation does not mark the dashboard current after the operator came back');
});
