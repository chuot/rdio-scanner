// Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.
//
// WebSocket API Access Policy:
// This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

// conformance.mjs drives the real web application, in a real browser, against a running server-ng.
//
// It is the live half of the checklist in
// docs/specs/FEAT-003-server-ng-refactor/quickstart.md §6: the listener's conversation with the server as the
// web application has it — the configuration arriving, a call appearing, the search, the unlock code — and the
// administrator's, through the pages the administrator uses. Nothing here reaches into the server: every claim
// is something a person using the application would see.
//
// The browser is driven over the DevTools protocol with nothing but node's own websocket and fetch, because
// the repository adds no test dependency for it and the point of this harness is the application as shipped.
//
// It knows nothing about the legacy server. Parity with it is answered by the pinned goldens under
// `testdata/golden` and the databases the legacy server wrote under `testdata/legacydb`; this harness answers a
// different question: does the client work against the server it is now embedded in.

import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync } from 'node:fs';
import { join } from 'node:path';

// ---------------------------------------------------------------------------
// The browser
// ---------------------------------------------------------------------------

class Browser {
  constructor(port, profileDir, downloadDir) {
    this.port = port;
    this.profileDir = profileDir;
    this.downloadDir = downloadDir;
    this.process = null;
    this.ws = null;
    this.nextId = 0;
    this.pending = new Map();
  }

  async launch() {
    if (!process.env.CHROME && !which('google-chrome')) {
      throw new Error('the conformance harness needs google-chrome on PATH');
    }

    this.process = spawn(
      chromeCommand(),
      [
        '--headless=new',
        '--no-sandbox',
        '--disable-gpu',
        '--disable-dev-shm-usage',
        '--disable-background-networking',
        '--disable-component-update',
        '--no-first-run',
        '--no-default-browser-check',
        '--disable-extensions',
        '--disable-popup-blocking',
        // A page the browser considers background gets its timers throttled, and the listener reconnects on
        // a timer: these flags keep the pages of a checklist running at full speed.
        '--disable-background-timer-throttling',
        '--disable-backgrounding-occluded-windows',
        '--disable-renderer-backgrounding',
        // The listener's audio context is created on the first user gesture, and a harness has no hands: this
        // is the flag that lets a headless browser play anyway.
        '--autoplay-policy=no-user-gesture-required',
        `--remote-debugging-port=${this.port}`,
        `--user-data-dir=${this.profileDir}`,
        '--window-size=1280,1000',
        'about:blank',
      ],
      { stdio: 'ignore' },
    );

    for (let attempt = 0; attempt < 80; attempt++) {
      try {
        const res = await fetch(`http://127.0.0.1:${this.port}/json/version`);
        const info = await res.json();

        this.ws = new WebSocket(info.webSocketDebuggerUrl);

        await new Promise((resolve, reject) => {
          this.ws.addEventListener('open', resolve);
          this.ws.addEventListener('error', reject);
        });

        this.ws.addEventListener('message', (event) => {
          const message = JSON.parse(event.data);

          if (message.id && this.pending.has(message.id)) {
            this.pending.get(message.id)(message);
            this.pending.delete(message.id);
          }
        });

        return this;
      } catch {
        await sleep(250);
      }
    }

    throw new Error('the browser never answered on its debugging port');
  }

  close() {
    try {
      this.ws?.close();
    } catch {
      // already gone
    }

    this.process?.kill('SIGINT');
  }

  send(method, params = {}) {
    const id = ++this.nextId;

    // A command the browser never answers must fail the harness, not hang it.
    const reply = new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);

        reject(new Error(`the browser never answered ${method}`));
      }, 30000);

      this.pending.set(id, (message) => {
        clearTimeout(timer);

        resolve(message);
      });
    });

    this.ws.send(JSON.stringify({ id, method, params }));

    return reply;
  }

  async targets() {
    const res = await fetch(`http://127.0.0.1:${this.port}/json/list`);

    return (await res.json()).filter((target) => target.type === 'page');
  }

  // The page this harness drives is the one it just asked the browser to create: a checklist that opens the
  // application twice must not end up driving a page it opened earlier.
  async openPage(url) {
    const before = new Set((await this.targets()).map((target) => target.id));

    const created = await this.send('Target.createTarget', { url });

    const wanted = created?.result?.targetId;

    for (let attempt = 0; attempt < 80; attempt++) {
      const found = (await this.targets()).find((target) =>
        wanted ? target.id === wanted : target.url === url && !before.has(target.id),
      );

      if (found) {
        // A page that is not the browser's active one is treated as background, so the page is brought to the
        // front before anything is asked of it.
        await this.send('Target.activateTarget', { targetId: found.id });

        return Page.attach(this, found);
      }

      await sleep(250);
    }

    throw new Error(`no page opened for ${url}`);
  }
}

function chromeCommand() {
  return process.env.CHROME || 'google-chrome';
}

function which(name) {
  try {
    const probe = spawn(name, ['--version'], { stdio: 'ignore' });

    return new Promise((resolve) => probe.on('error', () => resolve(false)).on('close', () => resolve(true)));
  } catch {
    return false;
  }
}

class Page {
  static async attach(browser, target) {
    const page = new Page(browser, target);

    await page.connect();

    return page;
  }

  constructor(browser, target) {
    this.browser = browser;
    this.url = target.url;
    this.ws = new WebSocket(target.webSocketDebuggerUrl);
    this.nextId = 0;
    this.pending = new Map();
  }

  async connect() {
    await new Promise((resolve, reject) => {
      this.ws.addEventListener('open', resolve);
      this.ws.addEventListener('error', reject);
    });

    this.ws.addEventListener('message', (event) => {
      const message = JSON.parse(event.data);

      if (message.id && this.pending.has(message.id)) {
        this.pending.get(message.id)(message);
        this.pending.delete(message.id);
      }
    });

    await this.send('Page.enable');
    await this.send('Runtime.enable');
    await this.send('Page.setDownloadBehavior', { behavior: 'allow', downloadPath: this.browser.downloadDir });
  }

  close() {
    try {
      this.ws?.close();
    } catch {
      // already gone
    }
  }

  send(method, params = {}) {
    const id = ++this.nextId;

    // A command the browser never answers must fail the harness, not hang it.
    const reply = new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);

        reject(new Error(`the browser never answered ${method}`));
      }, 30000);

      this.pending.set(id, (message) => {
        clearTimeout(timer);

        resolve(message);
      });
    });

    this.ws.send(JSON.stringify({ id, method, params }));

    return reply;
  }

  async evaluate(expression, { awaitPromise = false } = {}) {
    const reply = await this.send('Runtime.evaluate', {
      expression,
      awaitPromise,
      returnByValue: true,
    });

    if (reply.error) {
      throw new Error(`the browser refused ${expression.slice(0, 60)}: ${reply.error.message}`);
    }

    if (reply.exceptionDetails) {
      throw new Error(`the page raised: ${reply.exceptionDetails.exception?.description ?? reply.exceptionDetails.text}`);
    }

    // A DevTools reply nests the evaluated result inside a result object: reply.result is the
    // Runtime.evaluate result, and the value the expression produced is inside that.
    return reply.result?.result?.value;
  }

  async waitFor(expression, { timeout = 20000, what } = {}) {
    const until = Date.now() + timeout;

    while (Date.now() < until) {
      if (await this.evaluate(expression)) {
        return true;
      }

      await sleep(200);
    }

    // Whatever the page did show is part of the report: a harness that only says "it never appeared" is a
    // harness nobody can debug a week later.
    const seen = await this.evaluate(
      `JSON.stringify({ready: document.readyState, title: document.title, body: document.body?.innerText?.slice(0, 200) ?? null})`,
    ).catch((error) => JSON.stringify({ readingThePageFailed: String(error.message ?? error) }));

    throw new Error(
      `the page never showed ${what ?? 'what was waited for'}, within ${timeout}ms; the page holds ${seen}`,
    );
  }

  async goto(url) {
    await this.send('Page.navigate', { url });

    await this.waitFor('document.readyState === "complete"', { what: 'the page to finish loading' });
  }

  // The page is sent to the address it is already at, which is how a person reloads it. The command is
  // answered before the navigation happens, so the page is then waited for.
  async reload() {
    await this.send('Page.reload', { ignoreCache: true });

    await sleep(300);

    await this.waitFor('document.readyState === "complete"', { timeout: 30000, what: 'the page to finish loading' });
  }

  // A click the page cannot tell apart from a person's: the browser is told the mouse was pressed and
  // released over the element. That matters — the listener's audio engine only starts on a real gesture, and a
  // click synthesized inside the page is not one.
  async click(selector) {
    const box = await this.evaluate(`
      (() => {
        const node = document.querySelector(${JSON.stringify(selector)});

        if (!node) return null;

        node.scrollIntoView({block: 'center'});

        const rect = node.getBoundingClientRect();

        return {x: Math.round(rect.x + rect.width / 2), y: Math.round(rect.y + rect.height / 2)};
      })()
    `);

    if (box === null) {
      throw new Error(`there is no ${selector} on the page to click`);
    }

    for (const type of ['mousePressed', 'mouseReleased']) {
      await this.send('Input.dispatchMouseEvent', {
        type,
        x: box.x,
        y: box.y,
        button: 'left',
        clickCount: 1,
      });
    }
  }

  async fill(selector, value) {
    const done = await this.evaluate(`
      (() => {
        const node = document.querySelector(${JSON.stringify(selector)});

        if (!node) return false;

        node.value = ${JSON.stringify(value)};
        node.dispatchEvent(new Event('input', { bubbles: true }));

        return true;
      })()
    `);

    if (!done) {
      throw new Error(`there is no ${selector} on the page to fill`);
    }
  }

  async submit(selector) {
    const done = await this.evaluate(`
      (() => {
        const node = document.querySelector(${JSON.stringify(selector)});
        const form = node?.closest('form') ?? node;

        if (!form?.requestSubmit) return false;

        form.requestSubmit();

        return true;
      })()
    `);

    if (!done) {
      throw new Error(`there is no form at ${selector} to submit`);
    }
  }

  async text(selector) {
    return this.evaluate(`document.querySelector(${JSON.stringify(selector)})?.textContent ?? null`);
  }
}

// ---------------------------------------------------------------------------
// The server, reached the way an administrator and a recording tool reach it
// ---------------------------------------------------------------------------

class Server {
  constructor(base) {
    this.base = base;
    this.token = '';
  }

  async request(method, path, { body, token = this.token, form, expect } = {}) {
    const headers = {};

    if (token) {
      headers.Authorization = token;
    }

    const res = await fetch(this.base + path, {
      method,
      headers,
      body: form ? form : body === undefined ? undefined : JSON.stringify(body),
    });

    const text = await res.text();

    if (expect && !expect.includes(res.status)) {
      throw new Error(`${method} ${path} answered ${res.status}, want ${expect.join(' or ')}: ${text}`);
    }

    return { status: res.status, body: text };
  }

  async login(password) {
    const res = await this.request('POST', '/api/admin/login', { body: { password }, expect: [200] });

    this.token = JSON.parse(res.body).token;

    return this.token;
  }

  async json(path) {
    return JSON.parse((await this.request('GET', path, { expect: [200] })).body);
  }

  // upload posts one recording the way a recording tool posts it. A Buffer in the fields is the audio part,
  // an object is posted as its JSON, and everything else as text.
  async upload(path, fields) {
    const form = new FormData();

    for (const [name, value] of Object.entries(fields)) {
      if (Buffer.isBuffer(value)) {
        form.append(name, new Blob([value], { type: 'audio/wav' }), String(fields.audioFilename ?? 'capture.wav'));
      } else if (typeof value === 'object') {
        form.append(name, JSON.stringify(value));
      } else {
        form.append(name, String(value));
      }
    }

    return this.request('POST', path, { form, expect: [200] });
  }
}

// ---------------------------------------------------------------------------
// The checklist
// ---------------------------------------------------------------------------

const adminPassword = 'rdio-scanner';

// The password the administrator changes to, partway through the checklist.
const newPassword = 'conformance-new-password';

// A directory the dir watch section is allowed to name, given by run.sh so that a saved dir watch points at
// something that exists.
let watchDir = '';

// The configuration the harness gives a server, and the audio it uploads: nothing looks inside the recording, and
// the point is that the server passes the bytes it was given along.
const seed = JSON.parse(readFileSync(new URL('./fixtures/seed.json', import.meta.url), 'utf8'));

// recording is a real, if tiny, audio file: a second of 8 kHz mono 16-bit PCM. A harness that only compares what
// the server passed along would be content with meaningless bytes; this one puts the bytes in a browser, and a
// browser only plays audio it can decode.
function wavFile() {
  const rate = 8000;
  const samples = rate;
  const bytes = new Uint8Array(44 + samples * 2);
  const view = new DataView(bytes.buffer);

  const write = (at, text) => {
    for (let i = 0; i < text.length; i++) bytes[at + i] = text.charCodeAt(i);
  };

  write(0, 'RIFF');
  view.setUint32(4, 36 + samples * 2, true);
  write(8, 'WAVE');
  write(12, 'fmt ');
  view.setUint32(16, 16, true);
  view.setUint16(20, 1, true);
  view.setUint16(22, 1, true);
  view.setUint32(24, rate, true);
  view.setUint32(28, rate * 2, true);
  view.setUint16(32, 2, true);
  view.setUint16(34, 16, true);
  write(36, 'data');
  view.setUint32(40, samples * 2, true);

  for (let i = 0; i < samples; i++) {
    view.setInt16(44 + i * 2, Math.round(Math.sin((2 * Math.PI * 440 * i) / rate) * 8000), true);
  }

  return Buffer.from(bytes);
}

const recording = wavFile();

// A trunk-recorder pair: the sidecar beside the audio is what says what the call is. It names a different
// talkgroup from the upload above so that the display can be read as having changed.
const trunkRecorderSidecar = `{
  "short_name": "Grand Metro",
  "talkgroup": 1514100,
  "talkgroup_description": "Dispatch 2",
  "talkgroup_group": "Ground",
  "talkgroup_group_tag": "Dispatch",
  "talkgroup_tag": "Main",
  "timestamp": 1791288060000
}
`;

// The fields a recording tool actually posts to /api/call-upload.
function uploadFields(key) {
  return {
    key,
    system: 10001,
    talkgroup: 1514000,
    talkgroupLabel: 'Ground',
    talkgroupName: 'Dispatch 1',
    timestamp: 1791288000000,
    site: 101,
    frequencies: [{ dbm: -70, errorCount: 1, freq: 151400000, pos: 0.5, spikeCount: 2 }],
    patches: [1514100],
    sources: [{ pos: 0.5, src: 1001, tag: 'Car 1' }, { pos: 1.5, src: 1002, tag: 'Car 2' }],
    audio: recording,
    audioFilename: 'capture.wav',
    audioMime: 'audio/wav',
  };
}

class Conformance {
  constructor(server, browser) {
    this.server = server;
    this.browser = browser;
    this.pages = [];
    this.results = [];
  }

  page(url) {
    return this.browser.openPage(url).then((page) => {
      this.pages.push(page);

      return page;
    });
  }

  async item(name, run) {
    try {
      const evidence = await withDeadline(run(), `the item ${name}`);

      this.results.push({ name, ok: true, evidence });

      console.log(`  ok       ${name}: ${evidence}`);
    } catch (error) {
      this.results.push({ name, ok: false, evidence: String(error.message ?? error) });

      console.log(`  FAILED   ${name}: ${error.message ?? error}`);
    }
  }

  async close() {
    for (const page of this.pages) {
      page.close();
    }
  }
}

// ---------------------------------------------------------------------------
// The listener
// ---------------------------------------------------------------------------

async function listenerItems(app) {
  const base = app.server.base;

  let listener = null;

  await app.item('the-listener-opens-and-is-given-the-configuration', async () => {
    listener = await app.page(`${base}/`);

    // The first thing a listener does is open its socket, and a page whose socket is not open has nothing to
    // show yet. The status line says NO LINK until the socket is open, so that is what is waited for first.
    await listener.waitFor(`document.querySelector('rdio-display .link')?.textContent !== 'NO LINK'`, {
      timeout: 45000,
      what: 'the listener socket to open',
    });

    // The system list in the search filters is built from the configuration the server sent down the socket:
    // a listener that never got a configuration has "All Systems" and nothing else.
    await listener.waitFor(
      `Array.from(document.querySelectorAll('select[name="system"] option')).some((o) => o.textContent.includes('Grand Metro'))`,
      { timeout: 45000, what: 'the systems the server configured' },
    );

    const systems = await listener.evaluate(
      `Array.from(document.querySelectorAll('select[name="system"] option')).map((o) => o.textContent).join(', ')`,
    );

    return `the search filters offer ${systems}`;
  });

  await app.item('the-branding-the-administrator-set-is-shown', async () => {
    await listener.waitFor(`document.querySelector('rdio-status .branding')?.textContent === ${JSON.stringify(seed.options.branding)}`, {
      what: 'the branding line',
    });

    return `the status line reads ${JSON.stringify(await listener.text('rdio-status .branding'))}`;
  });

  await app.item('the-livefeed-is-turned-on', async () => {
    // A listener starts offline, in both clients: nothing is played until the person using it presses LIVE
    // FEED. Pressing it is also what sends the server the talkgroups this listener wants.
    await listener.click('button[data-action="livefeed"]');

    await listener.waitFor(`document.querySelector('button[data-action="livefeed"]').classList.contains('on')`, {
      what: 'the live feed switch to read on',
    });

    return 'the LIVE FEED switch reads on';
  });

  await app.item('a-call-uploaded-over-http-appears-live', async () => {
    const uploaded = await app.server.upload('/api/call-upload', uploadFields('harness-apikey-open'));

    await listener.waitFor(`document.querySelector('.call-name')?.textContent === 'Dispatch 1'`, {
      timeout: 30000,
      what: 'the talkgroup name of the call that was uploaded',
    });

    const shown = {
      system: await listener.text('.call-system'),
      talkgroup: await listener.text('.call-talkgroup'),
      name: await listener.text('.call-name'),
      unit: await listener.text('.call-unit'),
      led: await listener.evaluate(`document.querySelector('rdio-status .led')?.className`),
    };

    return `${uploaded.status} from the upload endpoint, and the display reads system ${shown.system}, talkgroup ${shown.talkgroup}, name ${JSON.stringify(shown.name)}, unit ${JSON.stringify(shown.unit)}, status light ${shown.led}`;
  });

  await app.item('a-recording-from-a-trunk-recorder-becomes-a-call', async () => {
    const form = new FormData();

    form.set('key', 'harness-apikey-open');
    form.set('meta', new Blob([trunkRecorderSidecar], { type: 'application/json' }), 'capture.json');
    form.set('audio', new Blob([recording], { type: 'audio/wav' }), 'capture.wav');

    const uploaded = await app.server.request('POST', '/api/trunk-recorder-call-upload', { form, expect: [200] });

    await listener.waitFor(`document.querySelector('.call-name')?.textContent === 'Dispatch 2'`, {
      timeout: 30000,
      what: 'the talkgroup name the trunk-recorder sidecar names',
    });

    return `${uploaded.status} for the pair, and the display reads system ${await listener.text('.call-system')}, talkgroup ${await listener.text('.call-talkgroup')}, named ${await listener.text('.call-name')}`;
  });

  await app.item('the-talkgroup-being-heard-can-be-held-and-released', async () => {
    // The switch classes are on and off; a substring test would read "on" inside "rdio-button", so the page is
    // asked for the class it has.
    const state = async () =>
      await listener.evaluate(
        `['on', 'off', 'partial'].filter((c) => document.querySelector('button[data-action="holdTalkgroup"]').classList.contains(c)).join('+')`,
      );

    const before = await state();

    await listener.click('button[data-action="holdTalkgroup"]');

    await listener.waitFor(
      `document.querySelector('button[data-action="holdTalkgroup"]').classList.contains('on')`,
      { what: 'the hold switch to read on' },
    );

    const held = await state();

    await listener.click('button[data-action="holdTalkgroup"]');

    await listener.waitFor(
      `document.querySelector('button[data-action="holdTalkgroup"]').classList.contains('off')`,
      { what: 'the hold switch to read off again' },
    );

    const released = await state();

    return `the switch went ${before} to ${held} and back to ${released}`;
  });

  await app.item('the-search-panel-returns-a-page-of-calls', async () => {
    await listener.click('button[data-action="search"]');

    await listener.waitFor(
      `document.querySelectorAll('rdio-search-panel tbody tr').length >= 2`,
      { timeout: 30000, what: 'the searched calls' },
    );

    const rows = await listener.evaluate(`
      Array.from(document.querySelectorAll('rdio-search-panel tbody tr')).map((row) =>
        Array.from(row.querySelectorAll('td span')).map((span) => span.textContent).join(' | '))
    `);

    const page = await listener.text('rdio-paginator .rdio-paginator-label');

    return `${rows.length} rows on ${page}: ${rows.slice(0, 2).join('  //  ')}`;
  });

  await app.item('a-searched-call-is-played-and-then-stopped', async () => {
    await listener.click('rdio-search-panel tbody tr button[data-play]');

    await listener.waitFor(
      `!!document.querySelector('rdio-search-panel tbody tr button[data-stop]')`,
      { timeout: 30000, what: 'the played call to offer a stop' },
    );

    const playing = await listener.text('.call-talkgroup');

    await listener.click('rdio-search-panel tbody tr button[data-stop]');

    await listener.waitFor(
      `!document.querySelector('rdio-search-panel tbody tr button[data-stop]')`,
      { what: 'the stop to clear' },
    );

    return `playing a searched call put talkgroup ${playing} on the display and offered a stop, which cleared`;
  });

  await app.item('a-searched-call-is-downloaded', async () => {
    rmSync(app.browser.downloadDir, { recursive: true, force: true });
    mkdirSync(app.browser.downloadDir, { recursive: true });

    await listener.click('rdio-search-panel .paginator rdio-toggle button');

    await listener.waitFor(
      `!!document.querySelector('rdio-search-panel tbody tr button[data-download]')`,
      { what: 'the rows to offer a download' },
    );

    await listener.click('rdio-search-panel tbody tr button[data-download]');

    const until = Date.now() + 20000;

    while (Date.now() < until) {
      const files = readdirSync(app.browser.downloadDir).filter((name) => name.length > 0);

      if (files.length > 0) {
        return `the download mode turned the row buttons into downloads, and the browser saved ${files.join(', ')}`;
      }

      await sleep(250);
    }

    throw new Error('the browser saved nothing from the download button within 20000ms');
  });

  await app.item('the-unlock-code-gate-refuses-a-wrong-code-and-takes-the-right-one', async () => {
    // An access is what makes the server ask a listener for a code. Saving the access section with the
    // identifiers the server gave it keeps the entries the server has, which is what a save that names nothing
    // new does in both servers.
    const stored = await app.server.json('/api/admin/config/access');

    stored.push({ id: 0, ident: 'Conformance gate', code: 'conformance-code', systems: '*' });

    await app.server.request('PUT', '/api/admin/config/access', { body: stored, expect: [200] });

    // A listener that has never been given a code is what the gate exists for, so the gate is tried with a
    // fresh page rather than with the one that has already been playing calls. The code a listener has been
    // given is remembered in local storage, so the page is made to forget it before the gate is tried.
    listener = await app.page(`${base}/`);

    await listener.evaluate(`localStorage.removeItem('rdio-scanner-pin')`);

    await listener.reload();

    await listener.waitFor(`document.querySelector('rdio-display .auth')?.classList.contains('visible')`, {
      timeout: 30000,
      what: 'the unlock overlay',
    });

    await listener.fill('#unlock-code', 'not-the-code');
    await listener.submit('#unlock-code');

    await listener.waitFor(
      `document.querySelector('rdio-display .auth rdio-form-field')?.textContent.includes('Invalid unlock code')`,
      { timeout: 30000, what: 'the refusal of the wrong code' },
    );

    const refused = await listener.text('rdio-display .auth rdio-form-field');

    await listener.fill('#unlock-code', 'conformance-code');
    await listener.submit('#unlock-code');

    await listener.waitFor(
      `!document.querySelector('rdio-display .auth')?.classList.contains('visible')`,
      { timeout: 30000, what: 'the unlock overlay to close' },
    );

    // The configuration a listener is given after the code is the scoped one, and the search still works.
    await listener.click('button[data-action="search"]');

    await listener.waitFor(`document.querySelectorAll('rdio-search-panel tbody tr').length >= 1`, {
      timeout: 30000,
      what: 'the searched calls behind the gate',
    });

    return `the overlay asked for a code, refused a wrong one with ${JSON.stringify(refused.trim())}, and let the application through with the right one`;
  });
}

// ---------------------------------------------------------------------------
// The administrator
// ---------------------------------------------------------------------------

const sections = ['options', 'systems', 'groups', 'tags', 'access', 'apikeys', 'dirwatch', 'downstreams'];

async function adminItems(app) {
  const base = app.server.base;

  await app.item('the-administrator-logs-in', async () => {
    const admin = await app.page(`${base}/admin`);

    await admin.waitFor(`!!document.querySelector('input[name="password"]')`, { what: 'the login form' });

    await admin.fill('input[name="password"]', adminPassword);
    await admin.submit('input[name="password"]');

    await admin.waitFor(`!!document.querySelector('nav[aria-label="Admin sections"] [data-section="systems"]')`, {
      timeout: 30000,
      what: 'the section navigation',
    });

    const nav = await admin.evaluate(
      `Array.from(document.querySelectorAll('nav[aria-label="Admin sections"] [data-section]')).map((n) => n.dataset.section).join(', ')`,
    );

    app.admin = admin;

    return `the login form accepted the password and the navigation offers ${nav}`;
  });

  await app.item('each-of-the-eight-sections-is-read-and-saved', async () => {
    const admin = app.admin;

    const saved = [];

    for (const section of sections) {
      await admin.click(`nav[aria-label="Admin sections"] [data-section="${section}"]`);

      // The save bar is built when the section's own data arrives, so its presence is the read half of the
      // round trip.
      await admin.waitFor(
        `!!document.querySelector('rdio-admin-section-bar[data-section="${section}"] button.save')`,
        { timeout: 30000, what: `the ${section} section to load` },
      );

      const edited = await editSomethingIn(admin, section);

      await admin.waitFor(
        `!document.querySelector('rdio-admin-section-bar[data-section="${section}"] button.save').disabled`,
        { timeout: 15000, what: `the ${section} save to become available after ${edited.how}` },
      );

      await admin.click(`rdio-admin-section-bar[data-section="${section}"] button.save`);

      await admin.waitFor(
        `document.querySelector('rdio-admin-section-bar[data-section="${section}"] button.save').disabled`,
        { timeout: 30000, what: `the ${section} save to finish` },
      );

      const read = await app.server.request('GET', `/api/admin/config/${section}`, { expect: [200] });

      saved.push(`${section}: ${edited.how}, ${read.body.length} bytes read back`);
    }

    return `each section was opened, changed with its own controls, saved, and read back over the api — ${saved.join('; ')}`;
  });

  await app.item('a-second-administrator-page-is-updated-by-a-save', async () => {
    const admin = app.admin;

    // The administrator token lives in sessionStorage, which is per page, so the second page logs in the way
    // a second browser tab does.
    const second = await app.page(`${base}/admin`);

    await second.waitFor(`!!document.querySelector('input[name="password"]')`, {
      what: 'the second page to ask for the password',
    });

    await second.fill('input[name="password"]', adminPassword);
    await second.submit('input[name="password"]');

    await second.waitFor(`!!document.querySelector('nav[aria-label="Admin sections"]')`, {
      timeout: 30000,
      what: 'the second page to reach the navigation',
    });

    await second.click(`nav[aria-label="Admin sections"] [data-section="groups"]`);

    await second.waitFor(`!!document.querySelector('rdio-admin-groups .rdio-admin-row')`, {
      timeout: 30000,
      what: 'the groups on the second page',
    });

    const before = await second.evaluate(
      `Array.from(document.querySelectorAll('rdio-admin-groups .rdio-admin-row')).map((row) => row.textContent.trim())`,
    );

    // The save happens on the first page.
    await admin.click(`nav[aria-label="Admin sections"] [data-section="groups"]`);

    await admin.waitFor(
      `!!document.querySelector('rdio-admin-section-bar[data-section="groups"] button.save')`,
      { timeout: 30000, what: 'the groups section on the first page' },
    );

    const edited = await editSomethingIn(admin, 'groups');

    await admin.waitFor(
      `!document.querySelector('rdio-admin-section-bar[data-section="groups"] button.save').disabled`,
      { timeout: 15000, what: `the groups save to become available after ${edited.how}` },
    );

    await admin.click(`rdio-admin-section-bar[data-section="groups"] button.save`);

    // The second page never asked for anything: the server pushed the configuration to it.
    await second.waitFor(
      `Array.from(document.querySelectorAll('rdio-admin-groups .rdio-admin-row')).some((row) => row.textContent.includes(${JSON.stringify(edited.marker)}))`,
      { timeout: 30000, what: `the second page to show the group named "${edited.marker}"` },
    );

    const after = await second.evaluate(
      `(() => {
        const rows = Array.from(document.querySelectorAll('rdio-admin-groups .rdio-admin-row'));
        const row = rows.find((r) => r.textContent.includes(${JSON.stringify(edited.marker)}));

        return {count: rows.length, row: row ? row.textContent.trim() : null};
      })()`,
    );

    return `the second page went from ${before.length} groups to ${after.count}, and now shows "${after.row}", without being asked to reload`;
  });

  await app.item('the-logs-page-shows-what-the-server-wrote', async () => {
    const admin = app.admin;

    await admin.click(`nav[aria-label="Admin sections"] [data-section="logs"]`);

    await admin.waitFor(
      `document.querySelectorAll('rdio-admin-logs tbody tr').length >= 1`,
      { timeout: 30000, what: 'the log rows' },
    );

    const rows = await admin.evaluate(
      `Array.from(document.querySelectorAll('rdio-admin-logs tbody tr')).slice(0, 3).map((row) => row.textContent.trim())`,
    );

    return `${rows.length} of the rows the server wrote, for example: ${JSON.stringify(rows[0])}`;
  });

  await app.item('the-administrator-changes-the-password-and-logs-in-with-it', async () => {
    const admin = app.admin;

    await admin.click(`nav[aria-label="Admin sections"] [data-section="password"]`);

    await admin.waitFor(`!!document.querySelector('rdio-admin-password input[name="newPassword"]')`, {
      timeout: 30000,
      what: 'the password change form',
    });

    await admin.fill('rdio-admin-password input[name="currentPassword"]', adminPassword);
    await admin.fill('rdio-admin-password input[name="newPassword"]', newPassword);
    await admin.fill('rdio-admin-password input[name="verifyNewPassword"]', newPassword);

    await admin.waitFor(`!document.querySelector('rdio-admin-password button.save').disabled`, {
      timeout: 15000,
      what: 'the password save to become available',
    });

    await admin.click('rdio-admin-password button.save');

    await admin.waitFor(`document.body.innerText.includes('Password changed successfully')`, {
      timeout: 30000,
      what: 'the page to report the change',
    });

    // An administrator token is signed with the password, so changing the password is what stops the old
    // password from working, in both servers.
    const refused = await app.server.request('POST', '/api/admin/login', {
      body: { password: adminPassword },
      expect: [401],
    });

    await app.server.login(newPassword);

    return `the page reported the change, the old password now answers ${refused.status}, and the new one logs in`;
  });

  await app.item('a-token-that-is-no-good-returns-to-the-login', async () => {
    const admin = app.admin;

    // The password change has invalidated the token this page holds, and the page may or may not have noticed
    // already. Either way the check is the same one a person gets: hand the page a token the server does not
    // accept and see where it ends up.
    if (!(await admin.evaluate(`!!document.querySelector('nav[aria-label="Admin sections"]')`))) {
      await admin.fill('input[name="password"]', newPassword);
      await admin.submit('input[name="password"]');

      await admin.waitFor(`!!document.querySelector('nav[aria-label="Admin sections"]')`, {
        timeout: 30000,
        what: 'the navigation after logging in again',
      });
    }

    await admin.evaluate(`sessionStorage.setItem('rdio-scanner-admin-token', 'not-a-token')`);

    // The token is read from storage whenever the page asks the server for something, so reloading the page is
    // what reveals it.
    await admin.reload();

    await admin.waitFor(`!!document.querySelector('input[name="password"]')`, {
      timeout: 30000,
      what: 'the login form after the server refused the token',
    });

    const left = await admin.evaluate(`sessionStorage.getItem('rdio-scanner-admin-token')`);

    if (left) {
      throw new Error(`the page kept the token the server refused: ${left}`);
    }

    return `a page holding a token the server refuses was put back at the login form and dropped the token`;
  });

  await app.item('the-administrator-logs-out', async () => {
    const admin = app.admin;

    await admin.fill('input[name="password"]', newPassword);
    await admin.submit('input[name="password"]');

    await admin.waitFor(`!!document.querySelector('nav[aria-label="Admin sections"]')`, {
      timeout: 30000,
      what: 'the navigation after logging in',
    });

    await admin.click('button.rdio-button.logout');

    await admin.waitFor(`!!document.querySelector('input[name="password"]')`, {
      timeout: 30000,
      what: 'the login form after the logout',
    });

    const token = await admin.evaluate(`sessionStorage.getItem('rdio-scanner-admin-token')`);

    if (token) {
      throw new Error(`the page still holds a token after the logout: ${token}`);
    }

    return `the logout control put the page back at the login form and left no token behind`;
  });
}

// editSomethingIn makes one section dirty using only what that section's own page offers. A section that
// already has rows gets one of their existing text fields changed, which is the change least likely to make a
// row invalid; a section that is empty gets a row from its own "new" control, and only the fields the page
// itself marks as required are filled. It reports what it changed and the text that proves the change.
async function editSomethingIn(page, section) {
  const changed = await page.evaluate(`
    (() => {
      const root = document.querySelector('rdio-admin-${section}');

      for (const input of root.querySelectorAll('input[type="text"]')) {
        if (!input.value) continue;

        input.value = input.value + ' conformance';
        input.dispatchEvent(new Event('input', { bubbles: true }));
        input.dispatchEvent(new Event('change', { bubbles: true }));

        return input.value;
      }

      return null;
    })()
  `);

  if (changed !== null) {
    return { how: `changed the text field holding "${changed}"`, marker: changed };
  }

  const added = await page.evaluate(`
    (() => {
      const root = document.querySelector('rdio-admin-${section}');
      const add = root.querySelector('button.add');

      if (!add) return false;

      add.click();

      return true;
    })()
  `);

  if (!added) {
    throw new Error(`the ${section} section has nothing filled in and no control to add a row`);
  }

  await sleep(300);

  const filled = await page.evaluate(`
    (() => {
      const root = document.querySelector('rdio-admin-${section}');
      const rows = root.querySelectorAll('.rdio-admin-row');
      const scope = rows.length > 0 ? rows[0] : root;

      const values = [];

      for (const input of scope.querySelectorAll('input[required], textarea[required]')) {
        if (input.value) continue;

        const field = input.closest('rdio-form-field');
        const label = (field?.querySelector('label')?.textContent ?? '').toLowerCase();

        let value = 'conformance-' + ${JSON.stringify(section)};

        if (label.includes('url')) value = 'http://127.0.0.1:3999/';
        else if (label.includes('directory')) value = ${JSON.stringify(watchDir)};
        else if (label.includes('key')) value = 'conformance-apikey';
        else if (label.includes('code')) value = 'conformance-code';

        input.value = value;
        input.dispatchEvent(new Event('input', { bubbles: true }));
        input.dispatchEvent(new Event('change', { bubbles: true }));

        values.push(value);
      }

      return values;
    })()
  `);

  if (filled.length === 0) {
    throw new Error(`the ${section} section added a row but this harness found no required field to fill`);
  }

  return { how: `added a row and filled ${filled.length} required field(s)`, marker: filled[0] };
}

// ---------------------------------------------------------------------------
// The drop-in: a database the legacy server wrote, served to the web application
// ---------------------------------------------------------------------------

async function dropInItems(app) {
  const base = app.server.base;

  await app.item('a-database-the-legacy-server-wrote-is-served-to-the-client', async () => {
    const listener = await app.page(`${base}/`);

    await listener.waitFor(`document.querySelector('rdio-display .link')?.textContent !== 'NO LINK'`, {
      timeout: 45000,
      what: 'the listener socket to open',
    });

    await listener.waitFor(
      `Array.from(document.querySelectorAll('select[name="system"] option')).some((o) => o.textContent.includes('Grand Metro'))`,
      { timeout: 45000, what: 'the systems the legacy server had stored' },
    );

    await listener.click('button[data-action="search"]');

    await listener.waitFor(`document.querySelectorAll('rdio-search-panel tbody tr').length >= 1`, {
      timeout: 30000,
      what: 'the calls the legacy server had stored',
    });

    const rows = await listener.evaluate(`
      Array.from(document.querySelectorAll('rdio-search-panel tbody tr')).slice(0, 3).map((row) =>
        Array.from(row.querySelectorAll('td span')).map((span) => span.textContent).join(' | '))
    `);

    await listener.click('rdio-search-panel tbody tr button[data-play]');

    await listener.waitFor(`!!document.querySelector('rdio-search-panel tbody tr button[data-stop]')`, {
      timeout: 30000,
      what: 'the stored call to start playing',
    });

    return `the configuration and the stored calls are there: ${rows.join('  //  ')}, and one of them plays`;
  });
}

// ---------------------------------------------------------------------------

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// Nothing in a checklist item may run forever. An item that stops making progress is a failed item, because a
// harness that hangs instead of failing is a harness nobody can run unattended.
function withDeadline(work, what, limit = 180000) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new Error(`${what} did not finish within ${limit}ms`));
    }, limit);

    Promise.resolve(work).then(
      (value) => {
        clearTimeout(timer);

        resolve(value);
      },
      (error) => {
        clearTimeout(timer);

        reject(error);
      },
    );
  });
}

function parseArgs(argv) {
  const parsed = { phase: 'main' };

  for (let i = 0; i < argv.length; i += 2) {
    parsed[argv[i].slice(2)] = argv[i + 1];
  }

  return parsed;
}

const args = parseArgs(process.argv.slice(2));

watchDir = args.watchDir ?? '';

const browser = new Browser(
  Number(args.cdpPort ?? 9333),
  mkdtempSync(join(args.work ?? '/tmp', 'conformance-chrome-')),
  mkdtempSync(join(args.work ?? '/tmp', 'conformance-downloads-')),
);

const server = new Server(args.base);

const app = new Conformance(server, browser);

let failed = 0;

try {
  if (args.phase === 'seed') {
    // The writing half of the drop-in: a server is given a configuration and a call, and is then left alone,
    // so that another server can be started on the database it wrote.
    await server.login(adminPassword);

    await server.request('PUT', '/api/admin/config', { body: seed, expect: [200] });

    const uploaded = await server.upload('/api/call-upload', uploadFields('harness-apikey-open'));

    console.log(`  ok       seeded and ingested one call: ${uploaded.status}`);
  } else if (args.phase === 'probe') {
    // A diagnostic for this harness itself: open the listener page and write down the frames the application
    // exchanges with the server. It is not part of the checklist.
    await browser.launch();

    const page = await app.page('about:blank#probe');

    await page.send('Network.enable');

    const frames = [];

    page.ws.addEventListener('message', (event) => {
      const message = JSON.parse(event.data);

      if (message.method === 'Network.webSocketFrameSent' || message.method === 'Network.webSocketFrameReceived') {
        const payload = message.params?.response?.payload ?? '';

        frames.push(`${message.method === 'Network.webSocketFrameSent' ? '->' : '<-'} ${payload.slice(0, 120)}`);
      }
    });

    // The socket is only observed from the moment Network is enabled, so the page is opened empty and sent to
    // the application afterwards.
    await page.goto(`${args.base}/`);

    await sleep(6000);

    console.log('  probe:', await page.evaluate(`JSON.stringify({ready: document.readyState, options: Array.from(document.querySelectorAll('select[name="system"] option')).map((o) => o.textContent)})`));
    console.log(frames.slice(0, 25).join('\n'));
  } else {
    await browser.launch();

    await server.login(adminPassword);

    if (args.phase === 'drop-in') {
      await dropInItems(app);
    } else {
      await server.request('PUT', '/api/admin/config', { body: seed, expect: [200] });

      await listenerItems(app);
      await adminItems(app);
    }
  }
} catch (error) {
  console.log(`  FAILED   the harness itself: ${error.message ?? error}`);

  failed++;
} finally {
  await app.close();

  browser.close();
}

const broken = app.results.filter((result) => !result.ok).length;

console.log('');
console.log('web application conformance');
console.log(`  items run: ${app.results.length}`);
console.log(`  items passed: ${app.results.length - broken}`);
console.log(`  items failed: ${broken}`);

process.exit(failed === 0 && broken === 0 ? 0 : 1);
