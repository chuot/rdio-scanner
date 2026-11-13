// dashboard refresh: interval, pause, visibility, config push, spoken output
// the interval timer is faked here and test-fired; other timers stay real

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

// counts status reads; some are held open so in-flight is a testable state
let statusReads = 0;
let held = false;
const pending = [];

// a refused read: the transport answers 417 with an empty body
let refused = false;

// the timestamp comes from the page clock, not a fixed instant:
// a fixed one reads fresh or stale depending on when the test ran
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

    if (refused) {
      return {
        status: 417,
        ok: false,
        async json() {
          throw new Error('no body');
        },
      };
    }

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

// the interval timer is fake: the test fires it rather than waiting
const holdTheRefreshInterval = () => holdRefreshInterval(STATUS_REFRESH_DELAY);

// advances the page clock: an hour in a background tab is not worth waiting for
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

// opens the page the way the shell does: session, config, active section
async function openDashboard() {
  window.sessionStorage.setItem(ADMIN_TOKEN_KEY, 'a-token-the-browser-is-holding');

  await getAdminContext().useCases.boot();

  const socket = sockets[sockets.length - 1];

  // The browser completing the handshake is what lets the server push to this page.
  socket.onopen?.();

  const el = new (customElements.get('rdio-admin-dashboard'))();

  el.connectedCallback();

  // awaits the open read: the use case holds it in flight, no second read starts
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

// the dashboard starts on auto-refresh when opened, and the control shows it on
test('auto-refresh is on when the operator opens the dashboard, and the control says it is on', async () => {
  const clock = holdTheRefreshInterval();

  let el;
  let socket;

  try {
    ({ el, socket } = await openDashboard());

    const control = refreshControl(el);

    assert.ok(control, 'the dashboard has no auto-refresh control');
    assert.match(control.textContent ?? '', /auto-refresh/i, `the control is labelled "${control.textContent}"`);
    assert.equal(control.getAttribute('aria-pressed'), 'true', 'the control does not say that auto-refresh is on');
    assert.equal((control.textContent ?? '').trim(), 'Auto-refresh on',
      `the control is pressed while its polling runs, but its own text reads "${control.textContent}"`);

    assert.equal(clock.waitingCount(), 1, `the page has ${clock.waitingCount()} refresh timers waiting, wanting exactly one`);
  } finally {
    // the page and its clock are let go whichever way the assertions went:
    // a failed reading must not leave a polling dashboard on the shared document
    el?.disconnectedCallback();
    socket?.close();

    clock.release();
  }
});

// pause stops the page asking; resume starts exactly one chain, not several
test('the pause control stops the page asking and resuming starts exactly one chain', async () => {
  const clock = holdTheRefreshInterval();

  try {
    const { el, socket } = await openDashboard();

    const control = refreshControl(el);

    statusReads = 0;

    control.dispatchEvent({ type: 'click' });

    assert.equal(control.getAttribute('aria-pressed'), 'false', 'the control does not say that auto-refresh is off');
    assert.equal((control.textContent ?? '').trim(), 'Auto-refresh off',
      `the control is unpressed after the pause, but its own text reads "${control.textContent}"`);
    assert.equal(clock.waitingCount(), 0, 'the page is still waiting to poll after the operator paused it');

    assert.equal(clock.fire(), 0, 'a paused dashboard polled anyway');
    assert.equal(statusReads, 0, `a paused dashboard asked the server ${statusReads} time(s)`);

    control.dispatchEvent({ type: 'click' });

    assert.equal(control.getAttribute('aria-pressed'), 'true');
    assert.equal((control.textContent ?? '').trim(), 'Auto-refresh on',
      `the control is pressed again after resuming, but its own text reads "${control.textContent}"`);
    assert.equal(clock.waitingCount(), 1, `resuming left ${clock.waitingCount()} refresh chains waiting, wanting one`);

    clock.fire();

    assert.equal(statusReads, 1, `resuming asked the server ${statusReads} times, wanting one read`);

    el.disconnectedCallback();
    socket.close();
  } finally {
    clock.release();
  }
});

// a poll never stacks: an older answer cannot replace a newer one
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

// a configuration save requests one read at once, not at the next interval
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

// a push and a poll landing together cost one request
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

// polling stops while the page is hidden: no reads for nobody on a hidden tab
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

// returning to the page reads staleness at once:
// waiting for the next poll to notice would be too late
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

// automatic refreshes stay silent; operator-initiated refreshes are announced
test('a refresh the page does by itself says nothing out loud, and one the operator asked for does', async () => {
  const clock = holdTheRefreshInterval();

  clearLiveRegion();

  try {
    const { el, socket } = await openDashboard();

    clock.fire();

    assert.equal(liveRegionText(), '', `a scheduled refresh said "${liveRegionText()}"`);

    readAgainControl(el).dispatchEvent({ type: 'click' });

    // awaits the operator's read: same in-flight read, with the answer attached first
    await getAdminContext().useCases.loadStatus();

    assert.match(liveRegionText(), /read/i, `the operator's own refresh said "${liveRegionText()}"`);

    el.disconnectedCallback();
    socket.close();
  } finally {
    clearLiveRegion();
    clock.release();
  }
});

// the page announces a refused scheduled read: the operator never asked to be told
test('a read the page makes on its own and the server refuses is said out loud', async () => {
  const clock = holdTheRefreshInterval();

  clearLiveRegion();

  try {
    const { el, socket } = await openDashboard();

    assert.equal(liveRegionText(), '', 'the page said something out loud before anything went wrong');

    refused = true;

    assert.equal(clock.fire(), 1, 'the page did not make the read its own schedule asked for');

    // The refusal the page is waiting on is the read its schedule started, and not a second one.
    await getAdminContext().useCases.loadStatus();

    assert.match(
      liveRegionText(),
      /did not answer/i,
      `the server stopped answering and the live region says "${liveRegionText()}"`,
    );

    el.disconnectedCallback();
    socket.close();
  } finally {
    clearLiveRegion();
    refused = false;
    clock.release();
  }
});

// a refusal is announced once; a repaint that is not a new refusal stays silent
test('the page says a refusal once, and a repaint that is not a new refusal says it again to nobody', async () => {
  const clock = holdTheRefreshInterval();

  clearLiveRegion();

  try {
    const { el, socket } = await openDashboard();

    refused = true;

    clock.fire();

    await getAdminContext().useCases.loadStatus();

    assert.match(liveRegionText(), /did not answer/i, `the page fell into the failure state without saying it: "${liveRegionText()}"`);

    // The next poll lands on a server that is still refusing: the same failure the page has already said.
    clock.fire();

    await getAdminContext().useCases.loadStatus();

    // A configuration push repaints the section, and a section being repainted is not a new failure.
    clearLiveRegion();

    socket.onmessage({ data: JSON.stringify({ ...CONFIG, groups: [{ id: 1, label: 'Dispatch' }] }) });

    await getAdminContext().useCases.loadStatus();

    assert.equal(
      liveRegionText(),
      '',
      `the page handed the operator the same refusal again for a repaint that was not a new refusal: "${liveRegionText()}"`,
    );

    el.disconnectedCallback();
    socket.close();
  } finally {
    clearLiveRegion();
    refused = false;
    clock.release();
  }
});

// re-asking while the server still refuses is answered again, not referred back
test('an operator asking again while the server is still refusing is answered again', async () => {
  const clock = holdTheRefreshInterval();

  clearLiveRegion();

  try {
    const { el, socket } = await openDashboard();

    refused = true;

    clock.fire();

    await getAdminContext().useCases.loadStatus();

    assert.match(liveRegionText(), /did not answer/i, `the page fell into the failure state without saying it: "${liveRegionText()}"`);

    clearLiveRegion();

    readAgainControl(el).dispatchEvent({ type: 'click' });

    await getAdminContext().useCases.loadStatus();

    assert.match(
      liveRegionText(),
      /did not answer/i,
      `the operator asked again while the server was refusing and the page said "${liveRegionText()}"`,
    );

    el.disconnectedCallback();
    socket.close();
  } finally {
    clearLiveRegion();
    refused = false;
    clock.release();
  }
});

// the refresh control works whether or not the page is polling on its own
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

// a section the shell replaced clears its interval
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
