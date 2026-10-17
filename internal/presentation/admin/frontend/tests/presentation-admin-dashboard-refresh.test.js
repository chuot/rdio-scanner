// The dashboard keeping itself current (presentation/admin/rdio-admin-dashboard.js): the interval the page
// polls on, the operator's own pause control, the page going away and coming back, the configuration push
// that asks for an answer at once, and what the page says out loud when the operator asked for a read and
// what it says nothing about when its own schedule did.
//
// The page's own timer is held here: the dashboard's interval is taken off the clock and fired by the test,
// so no test in this file waits for anything. Every other timer on the page — a socket reconnect, the test
// runner's own — is left on the real clock.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';
import { ADMIN_TOKEN_KEY } from '../infrastructure/token-storage.js';

import { STATUS_REFRESH_DELAY, STATUS_STALE_AFTER_MS } from '../domain/dashboard-status.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-dashboard.js';

import { findAll, setPageVisibility } from './dom-shim.js';
import { holdRefreshInterval } from './refresh-clock.js';

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
  live: { listeners: 3, administratorSessions: 1, dirwatchersRunning: 2, heldCalls: 2, audioConversion: true, audioProbe: true },
  activity: {
    windowStart: '2026-10-16T08:00:00Z',
    windowEnd: '2026-10-17T08:00:00Z',
    callsToday: 214,
    callsLastHour: 37,
    lastCallAt: '2026-10-17T08:44:02Z',
    hours: Array.from({ length: 24 }, (_, index) => ({ start: '2026-10-16T08:00:00Z', count: 1 })),
    busiest: [],
    recent: [],
  },
  logs: { windowStart: '2026-10-16T08:45:11Z', errors: 0, warnings: 0, sources: [] },
};

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

// The status reads the page asked for, and the ones the test is holding open so that "in flight" is a
// state a test can actually stand in.
let statusReads = 0;
let held = false;
const pending = [];

// The moment a status read names. Whether the page thinks the figures it is holding have gone stale is a
// comparison against the page's own clock, so the fixture names the moment the read is happening rather
// than an instant copied from a printed page: a fixed one would be current or stale depending on when the
// test ran.
let statusNow = new Date().toISOString();

function servedStatus() {
  return { ...STATUS, server: { ...STATUS.server, now: statusNow } };
}

function answer(body) {
  return {
    status: 200,
    ok: true,
    async json() {
      return structuredClone(body);
    },
  };
}

globalThis.fetch = async (url) => {
  const path = String(url);

  if (path.endsWith('/api/admin/config')) return answer({ docker: false, passwordNeedChange: false, config: CONFIG });
  if (path.endsWith('/api/admin/alerts')) return answer({});

  if (path.endsWith('/api/admin/status')) {
    statusReads++;

    if (held) return new Promise((resolve) => pending.push(resolve));

    return answer(servedStatus());
  }

  return answer({});
};

function releaseHeld() {
  held = false;

  for (const resolve of pending.splice(0)) resolve(answer(servedStatus()));
}

// The configuration socket, held the same way: the test decides when the server pushed.
const sockets = [];

globalThis.WebSocket = class FakeAdminSocket {
  constructor(url) {
    this.url = url;
    this.sent = [];

    sockets.push(this);
  }

  send(message) {
    this.sent.push(message);
  }

  close() {
    this.onclose?.({ code: 1000 });
  }
};

// The dashboard's interval, taken off the clock, so that "the interval arrived" is something the test says
// rather than something the test waits for.
const holdTheRefreshInterval = () => holdRefreshInterval(STATUS_REFRESH_DELAY);

// The page's own clock, moved forward. An operator who left this section behind another tab and came back
// an hour later is a thing no test waits for, and the clock the page measures its figures against is the
// one thing that moved while they were gone.
function withPageClockAdvancedBy(offsetMs, moveThePage) {
  const RealDate = globalThis.Date;

  class AdvancedDate extends RealDate {
    constructor(...args) {
      if (args.length === 0) super(RealDate.now() + offsetMs);

      else super(...args);
    }
  }

  globalThis.Date = AdvancedDate;

  try {
    moveThePage();
  } finally {
    globalThis.Date = RealDate;
  }
}

function readLineText(el) {
  const line = findAll(el, (node) => node.classList?.contains('rdio-dashboard-read'))[0];

  const words = (node) => [node._text ?? '', ...(node.children ?? []).map(words)].join('');

  return line ? words(line).trim() : '';
}

// The page, opened the way the shell opens it: a session the browser holds, the configuration the session
// starts with, and the section the operator is standing on.
async function openDashboard() {
  window.sessionStorage.setItem(ADMIN_TOKEN_KEY, 'a-token-the-browser-is-holding');

  await getAdminContext().useCases.boot();

  const socket = sockets[sockets.length - 1];

  // The browser completing the handshake is what lets the server push to this page.
  socket.onopen?.();

  const el = new (customElements.get('rdio-admin-dashboard'))();

  el.connectedCallback();

  // The read the section makes when it opens, awaited: the use case holds the read it has in flight, so this
  // is the very same read and not a second one, and the section is left with its answer settled before a
  // test starts driving its schedule.
  await getAdminContext().useCases.loadStatus();

  return { el, socket };
}

function refreshControl(el) {
  return findAll(el, (node) => node.classList?.contains('rdio-dashboard-auto-refresh'))[0] ?? null;
}

function readAgainControl(el) {
  return findAll(el, (node) => node.tag === 'button' && node.classList?.contains('rdio-dashboard-refresh'))[0] ?? null;
}

function liveRegionText() {
  const region = findAll(globalThis.document.body, (node) => node.tag === 'rdio-live-region')[0];

  return region?.textContent ?? '';
}

function clearLiveRegion() {
  globalThis.document.body.children = [];
}

// The operator who opens the dashboard expects it to keep up by itself: the page is showing what the
// server is doing now, and a page that stopped reading an hour ago is not showing that.
test('auto-refresh is on when the operator opens the dashboard, and the control says it is on', async () => {
  const clock = holdTheRefreshInterval();

  try {
    const { el, socket } = await openDashboard();

    const control = refreshControl(el);

    assert.ok(control, 'the dashboard has no auto-refresh control');
    assert.match(control.textContent ?? '', /auto-refresh/i, `the control is labelled "${control.textContent}"`);
    assert.equal(control.getAttribute('aria-pressed'), 'true', 'the control does not say that auto-refresh is on');

    assert.equal(clock.waitingCount(), 1, `the page has ${clock.waitingCount()} refresh timers waiting, wanting exactly one`);

    el.disconnectedCallback();
    socket.close();
  } finally {
    clock.release();
  }
});

// The control is the operator's own decision, and it is one control: pressing it stops the page asking,
// pressing it again starts exactly one chain of asking and not the two or three the page may have started
// along the way.
test('the pause control stops the page asking and resuming starts exactly one chain', async () => {
  const clock = holdTheRefreshInterval();

  try {
    const { el, socket } = await openDashboard();

    const control = refreshControl(el);

    statusReads = 0;

    control.dispatchEvent({ type: 'click' });

    assert.equal(control.getAttribute('aria-pressed'), 'false', 'the control does not say that auto-refresh is off');
    assert.equal(clock.waitingCount(), 0, 'the page is still waiting to poll after the operator paused it');

    assert.equal(clock.fire(), 0, 'a paused dashboard polled anyway');
    assert.equal(statusReads, 0, `a paused dashboard asked the server ${statusReads} time(s)`);

    control.dispatchEvent({ type: 'click' });

    assert.equal(control.getAttribute('aria-pressed'), 'true');
    assert.equal(clock.waitingCount(), 1, `resuming left ${clock.waitingCount()} refresh chains waiting, wanting one`);

    clock.fire();

    assert.equal(statusReads, 1, `resuming asked the server ${statusReads} times, wanting one read`);

    el.disconnectedCallback();
    socket.close();
  } finally {
    clock.release();
  }
});

// The status read is several statements over a history that can hold years of calls. A poll that fires
// while a read is still running stacks a second one on top of it, and the page ends up answering itself
// with an older number over a newer one.
test('a poll is not stacked on a read that is still running', async () => {
  const clock = holdTheRefreshInterval();

  try {
    const { el, socket } = await openDashboard();

    statusReads = 0;

    held = true;

    clock.fire();

    assert.equal(statusReads, 1, 'the interval did not ask the server');

    // The next interval arrives while the first read is still running.
    clock.fire();

    assert.equal(statusReads, 1, `the page stacked ${statusReads} reads on top of one another`);
    assert.equal(clock.waitingCount(), 1, 'a skipped poll ended the page own refresh');

    releaseHeld();

    el.disconnectedCallback();
    socket.close();
  } finally {
    releaseHeld();
    clock.release();
  }
});

// A configuration save changes what the server is doing — a watch is turned on, a downstream is removed —
// and the dashboard is the page that shows what the server is doing. Waiting for the next interval to
// find out what a save the operator just made did is a page that shows the operator a stale answer on
// purpose.
test('a configuration push asks the server once, at once', async () => {
  const clock = holdTheRefreshInterval();

  try {
    const { el, socket } = await openDashboard();

    statusReads = 0;

    socket.onmessage({ data: JSON.stringify({ ...CONFIG, groups: [{ id: 1, label: 'Dispatch' }] }) });

    assert.equal(statusReads, 1, `a configuration push asked the server ${statusReads} times, wanting one read`);

    el.disconnectedCallback();
    socket.close();
  } finally {
    clock.release();
  }
});

// The two ways the page asks can arrive in the same moment: the interval fires and the operator's save
// lands. Two reads of the same instant are one request too many on a read that costs several statements
// over the history.
test('a configuration push and a poll in the same moment cost one request', async () => {
  const clock = holdTheRefreshInterval();

  try {
    const { el, socket } = await openDashboard();

    statusReads = 0;

    held = true;

    clock.fire();

    assert.equal(statusReads, 1, 'the interval did not ask the server');

    socket.onmessage({ data: JSON.stringify({ ...CONFIG, groups: [{ id: 1, label: 'Dispatch' }] }) });

    assert.equal(statusReads, 1, `the push and the poll cost ${statusReads} requests, wanting one`);

    releaseHeld();

    el.disconnectedCallback();
    socket.close();
  } finally {
    releaseHeld();
    clock.release();
  }
});

// A dashboard in a tab behind another one, or on a laptop with the lid shut, is a page asking the server
// several statements every five seconds for nobody. The browser says when the page stopped being the page
// the operator is looking at, and the page stops.
test('the page stops asking while it is not the page the operator is looking at', async () => {
  const clock = holdTheRefreshInterval();

  try {
    const { el, socket } = await openDashboard();

    statusReads = 0;

    setPageVisibility('hidden');

    assert.equal(clock.waitingCount(), 0, 'the page is still waiting to poll while it is not visible');

    assert.equal(clock.fire(), 0, 'a page nobody is looking at polled anyway');
    assert.equal(statusReads, 0, `a page nobody is looking at asked the server ${statusReads} time(s)`);

    setPageVisibility('visible');

    assert.equal(clock.waitingCount(), 1, `coming back left ${clock.waitingCount()} refresh chains waiting, wanting one`);

    clock.fire();

    assert.equal(statusReads, 1, `coming back asked the server ${statusReads} times, wanting one read`);

    el.disconnectedCallback();
    socket.close();
  } finally {
    setPageVisibility('visible');
    clock.release();
  }
});

// A page that stopped asking has also stopped keeping its figures current, and the figures stay on the
// screen while it does. The line that attaches a moment to every one of them is re-read at the moment the
// operator looks at the page again, rather than waiting for the next poll to notice.
test('the page says the figures it is holding have gone stale when the operator comes back to them', async () => {
  const clock = holdTheRefreshInterval();

  try {
    const { el, socket } = await openDashboard();

    assert.doesNotMatch(
      readLineText(el),
      /no longer keeping it current/,
      `the page calls its own figures stale before any of them have aged: "${readLineText(el)}"`,
    );

    setPageVisibility('hidden');

    withPageClockAdvancedBy(STATUS_STALE_AFTER_MS + 1000, () => setPageVisibility('visible'));

    assert.match(
      readLineText(el),
      /no longer keeping it current/,
      `coming back to a page whose figures have aged says "${readLineText(el)}"`,
    );

    el.disconnectedCallback();
    socket.close();
  } finally {
    setPageVisibility('visible');
    clock.release();
  }
});

// A refresh the page does by itself is the page doing what it was told to do: saying so every five seconds
// into a screen reader's voice is a page that never stops talking. A refresh the operator asked for is an
// answer to the operator, and it is answered out loud.
test('a refresh the page does by itself says nothing out loud, and one the operator asked for does', async () => {
  const clock = holdTheRefreshInterval();

  clearLiveRegion();

  try {
    const { el, socket } = await openDashboard();

    clock.fire();

    assert.equal(liveRegionText(), '', `a scheduled refresh said "${liveRegionText()}"`);

    readAgainControl(el).dispatchEvent({ type: 'click' });

    // The read the operator asked for, awaited: the use case holds the read in flight, so this is the same
    // read, and the page's own answer-out-loud continuation is attached before this one.
    await getAdminContext().useCases.loadStatus();

    assert.match(liveRegionText(), /read/i, `the operator's own refresh said "${liveRegionText()}"`);

    el.disconnectedCallback();
    socket.close();
  } finally {
    clearLiveRegion();
    clock.release();
  }
});

// The operator can ask for an answer right now whatever the schedule is doing, and a page whose schedule
// is paused is still a page that can answer a question.
test('the operator can ask for a read whether or not the page is polling on its own', async () => {
  const clock = holdTheRefreshInterval();

  try {
    const { el, socket } = await openDashboard();

    refreshControl(el).dispatchEvent({ type: 'click' });

    statusReads = 0;

    readAgainControl(el).dispatchEvent({ type: 'click' });

    assert.equal(statusReads, 1, `the operator's read asked the server ${statusReads} times, wanting one`);

    assert.equal(clock.fire(), 0, 'the page started polling again on its own');

    el.disconnectedCallback();
    socket.close();
  } finally {
    clock.release();
  }
});

// A section the shell has replaced is a section that is no longer on the page: its timer is the page
// asking questions on a schedule nobody can see or stop.
test('a dashboard the operator leaves stops asking', async () => {
  const clock = holdTheRefreshInterval();

  try {
    const { el, socket } = await openDashboard();

    statusReads = 0;

    el.disconnectedCallback();

    assert.equal(clock.waitingCount(), 0, 'the page left on the screen is still waiting to poll');

    assert.equal(clock.fire(), 0, 'a dashboard the operator left polled anyway');
    assert.equal(statusReads, 0, `a dashboard the operator left asked the server ${statusReads} time(s)`);

    socket.close();
  } finally {
    clock.release();
  }
});
