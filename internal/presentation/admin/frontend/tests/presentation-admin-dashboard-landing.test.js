// the landing is proven through the two real session starts through
// the shell's own context, adapter, and an answered transport.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';
import { ADMIN_TOKEN_KEY } from '../infrastructure/token-storage.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin.js';

import { findAll } from './dom-shim.js';

// the shell's context is built once, on first ask, and its adapter keeps that one
// so the transport must be answered before any component asks
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

// sign-out goes through the page's own store: the store is what says signed out
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

// the nav icon is icon-font ligature text, hidden from readers
// so the label is the span, not the whole button
function navLabel(button) {
  const label = findAll(button, (node) => node.tag === 'span')[0];

  return (label?.textContent ?? button.textContent ?? '').trim();
}

// the dashboard leads: it holds what an operator needs before choosing anything
test('an operator whose browser already holds a session lands on the dashboard', async () => {
  window.sessionStorage.setItem(ADMIN_TOKEN_KEY, 'a-token-the-browser-is-holding');

  const el = openShell();

  await getAdminContext().useCases.boot();

  const landed = sectionOnPage(el);

  assert.equal(landed?.tag, 'rdio-admin-dashboard', `the page landed on <${landed?.tag ?? 'nothing'}>, not on the dashboard`);
  assert.equal(headingOf(el)?.textContent.trim(), 'Dashboard', 'the heading does not name the section the operator landed on');
});

// the password sign-in start lands the same: one shell answers both ways
test('an operator who has just signed in lands on the dashboard', async () => {
  signOut();

  const el = openShell();

  const result = await getAdminContext().useCases.login('the-operator-typed-this');

  assert.equal(result, 'ok', 'the sign-in did not succeed on this fixture');

  const landed = sectionOnPage(el);

  assert.equal(landed?.tag, 'rdio-admin-dashboard', `the page landed on <${landed?.tag ?? 'nothing'}>, not on the dashboard`);
  assert.equal(headingOf(el)?.textContent.trim(), 'Dashboard');
});

// signed out, only the sign-in page shows: the status read is for members only
test('an operator who is not signed in is not shown the dashboard', () => {
  signOut();

  const asked = requests.length;

  const el = openShell();

  assert.equal(sectionOnPage(el)?.tag, undefined, 'the page put a section on the sign-in page');

  assert.ok(findAll(el, (node) => node.tag === 'rdio-admin-login').length, 'the page does not carry the sign-in panel');

  // the status read is several history statements, open to any caller
  // a page not let in asks the server for nothing
  assert.equal(requests.length, asked, `the sign-in page asked the server for ${requests.length - asked} thing(s) it has not earned`);
});

// the dashboard heads the nav after either session start: operators see
// where they stand and find the landing at the column's head.
test('the dashboard heads the navigation, whichever way the session started', async () => {
  const opensWithTheLanding = (items, how) => {
    const sections = items.map((button) => button.dataset.section);

    assert.equal(sections[0], 'dashboard', `the navigation opens with ${navLabel(items[0]) ?? 'nothing'}, not with the landing, on ${how}`);
    assert.equal(navLabel(items[0]), 'Dashboard', `the first item in the navigation is not labelled Dashboard, on ${how}`);
    assert.equal(sections[1], 'systems', `the section at the top of the hierarchy is not the one after the landing, on ${how}`);
  };

  window.sessionStorage.setItem(ADMIN_TOKEN_KEY, 'a-token-the-browser-is-holding');

  const booted = openShell();

  await getAdminContext().useCases.boot();

  opensWithTheLanding(navButtons(booted), 'the session the browser was already holding');

  signOut();

  const signedIn = openShell();

  assert.equal(await getAdminContext().useCases.login('the-operator-typed-this'), 'ok', 'the sign-in did not succeed on this fixture');

  opensWithTheLanding(navButtons(signedIn), 'the sign-in the operator just finished');
});

// the landing is the nav's current item until another section is chosen
// one current item at a time
test('the landing is the item the navigation marks as current', async () => {
  const marksOnlyTheLanding = (el, how) => {
    const currents = navButtons(el).filter((button) => button.getAttribute('aria-current') !== null);

    assert.equal(currents.length, 1, `the navigation marks ${currents.length} items as current, on ${how}`);
    assert.equal(currents[0].getAttribute('aria-current'), 'page');
    assert.equal(currents[0].dataset.section, 'dashboard', 'the item marked current is not the dashboard the operator landed on');
  };

  window.sessionStorage.setItem(ADMIN_TOKEN_KEY, 'a-token-the-browser-is-holding');

  const booted = openShell();

  await getAdminContext().useCases.boot();

  marksOnlyTheLanding(booted, 'the session the browser was already holding');

  signOut();

  const signedIn = openShell();

  assert.equal(await getAdminContext().useCases.login('the-operator-typed-this'), 'ok', 'the sign-in did not succeed on this fixture');

  marksOnlyTheLanding(signedIn, 'the sign-in the operator just finished');
});

// the landing behaves like any section: leave it, return, the same page again
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
