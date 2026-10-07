/*
 * *****************************************************************************
 * Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@chrystian.huot>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>
 * ****************************************************************************
 */

/*
 * The parity probe.
 *
 * It opens the current scanner application and the legacy reference application in the same headless Chrome, puts
 * both through the same sequence of states at the same viewports, reads the box and the computed style of every
 * surface named in surfaces.mjs, and writes a matrix of what differs.
 *
 * The browser is driven over the DevTools protocol with nothing but Node's own WebSocket and fetch, the way
 * ../conformance/conformance.mjs drives it: no driver package, no test framework. Clicks are real mouse presses,
 * because several of these surfaces — the drawer, the snack bar, the audio engine — only respond to an input the
 * page cannot tell apart from a person's.
 *
 * Exit codes: 0 every measured row matches, 1 the probe ran and at least one row differs, 2 the probe could not
 * measure what it was asked to measure. A missing surface is exit 2, never a blank cell.
 */

import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { spawn } from 'node:child_process';
import {
  adminPairs,
  adminStates,
  computedProperties,
  controls,
  pairs,
  states,
  tolerance,
  viewports,
} from './surfaces.mjs';

// ---------------------------------------------------------------------------
// The browser
// ---------------------------------------------------------------------------

class Browser {
  constructor(port, profileDir) {
    this.port = port;
    this.profileDir = profileDir;
    this.process = null;
    this.ws = null;
    this.nextId = 0;
    this.pending = new Map();
  }

  async launch() {
    // A browser left over from an earlier run holds the debugging port, and attaching to it would measure the
    // previous run's pages against servers that have long gone away.
    const stale = await fetch(`http://127.0.0.1:${this.port}/json/version`).then(() => true).catch(() => false);

    if (stale) {
      throw new Error(
        `something is already answering on the debugging port ${this.port}, most likely a browser left over ` +
          'from an earlier run; nothing this harness wrote can be trusted until it is gone',
      );
    }

    this.process = spawn(
      process.env.CHROME || 'google-chrome',
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
        '--disable-background-timer-throttling',
        '--disable-backgrounding-occluded-windows',
        '--disable-renderer-backgrounding',
        '--autoplay-policy=no-user-gesture-required',
        '--hide-scrollbars',
        '--force-device-scale-factor=1',
        `--remote-debugging-port=${this.port}`,
        `--user-data-dir=${this.profileDir}`,
        '--window-size=1280,1024',
        'about:blank',
      ],
      { stdio: 'ignore' },
    );

    for (let attempt = 0; attempt < 80; attempt++) {
      try {
        const info = await (await fetch(`http://127.0.0.1:${this.port}/json/version`)).json();

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

  async close() {
    // Chrome is asked to shut itself down first, because a browser left running holds the debugging port and the
    // next run would attach to it instead of to its own.
    try {
      await Promise.race([this.send('Browser.close'), sleep(1000)]);
    } catch {
      // the browser was already gone
    }

    try {
      this.ws?.close();
    } catch {
      // already gone
    }

    this.process?.kill('SIGKILL');
  }

  send(method, params = {}) {
    return call(this, method, params);
  }

  async openPage(url) {
    const created = await this.send('Target.createTarget', { url });
    const wanted = created?.result?.targetId;

    for (let attempt = 0; attempt < 80; attempt++) {
      const list = await (await fetch(`http://127.0.0.1:${this.port}/json/list`)).json();
      const found = list.find((target) => target.type === 'page' && target.id === wanted);

      if (found) {
        await this.send('Target.activateTarget', { targetId: found.id });

        return Page.attach(this, found);
      }

      await sleep(250);
    }

    throw new Error(`no page opened for ${url}`);
  }
}

function call(channel, method, params) {
  const id = ++channel.nextId;

  const reply = new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      channel.pending.delete(id);

      reject(new Error(`the browser never answered ${method}`));
    }, 30000);

    channel.pending.set(id, (message) => {
      clearTimeout(timer);

      resolve(message);
    });
  });

  channel.ws.send(JSON.stringify({ id, method, params }));

  return reply;
}

class Page {
  static async attach(browser, target) {
    const page = new Page(browser, target);

    await page.connect();

    return page;
  }

  constructor(browser, target) {
    this.browser = browser;
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

      // Both applications refuse to be navigated away from while the live feed is on, and a headless browser left
      // with that question open never finishes the navigation. The answer is always the same: leave.
      if (message.method === 'Page.javascriptDialogOpening') {
        this.send('Page.handleJavaScriptDialog', { accept: true }).catch(() => false);

        return;
      }

      if (message.id && this.pending.has(message.id)) {
        this.pending.get(message.id)(message);
        this.pending.delete(message.id);
      }
    });

    await this.send('Page.enable');
    await this.send('Runtime.enable');
  }

  send(method, params = {}) {
    return call(this, method, params);
  }

  async evaluate(expression) {
    const reply = await this.send('Runtime.evaluate', { expression, returnByValue: true });

    if (reply.error) {
      throw new Error(`the browser refused the probe: ${reply.error.message}`);
    }

    if (reply.exceptionDetails) {
      throw new Error(`the page raised: ${reply.exceptionDetails.exception?.description ?? reply.exceptionDetails.text}`);
    }

    return reply.result?.result?.value;
  }

  async waitFor(expression, { timeout = 25000, what } = {}) {
    const until = Date.now() + timeout;

    while (Date.now() < until) {
      if (await this.evaluate(expression)) {
        return true;
      }

      await sleep(200);
    }

    const seen = await this.evaluate(
      `JSON.stringify({ready: document.readyState, title: document.title, body: document.body?.innerText?.slice(0, 200) ?? null})`,
    ).catch((error) => JSON.stringify({ readingThePageFailed: String(error.message ?? error) }));

    throw new Error(`the page never showed ${what ?? 'what was waited for'} within ${timeout}ms; it holds ${seen}`);
  }

  async goto(url) {
    await this.send('Page.navigate', { url });

    await this.waitFor('document.readyState === "complete"', { what: 'the page to finish loading' });
  }

  async setViewport({ width, height }) {
    await this.send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: false });

    await sleep(350);
  }

  async setUserAgent(userAgent) {
    await this.send('Network.setUserAgentOverride', { userAgent });
  }

  // The element is found in the page, and then the mouse is pressed and released over it. A click synthesized
  // inside the page is not a user gesture, and several of these surfaces only answer to one.
  async click(locator, { settle = 250 } = {}) {
    const box = await this.evaluate(`
      (() => {
        const nodes = [...document.querySelectorAll(${JSON.stringify(locator.css)})];
        const node = ${locator.text ? `nodes.find((n) => new RegExp(${JSON.stringify(locator.text)}).test(n.textContent ?? ''))` : 'nodes[0]'};

        if (!node) return null;

        node.scrollIntoView({block: 'center'});

        const rect = node.getBoundingClientRect();

        if (rect.width === 0 || rect.height === 0) return {hidden: true};

        return {x: Math.round(rect.x + rect.width / 2), y: Math.round(rect.y + rect.height / 2)};
      })()
    `);

    if (box === null) {
      throw new Error(`there is no ${locator.css} on the page to press`);
    }

    if (box.hidden) {
      throw new Error(`${locator.css} is on the page but has no size to press`);
    }

    for (const type of ['mousePressed', 'mouseReleased']) {
      await this.send('Input.dispatchMouseEvent', { type, x: box.x, y: box.y, button: 'left', clickCount: 1 });
    }

    if (settle) {
      await sleep(settle);
    }
  }

  async type(locator, value) {
    const done = await this.evaluate(`
      (() => {
        const node = document.querySelector(${JSON.stringify(locator.css)});

        if (!node) return false;

        node.focus();
        node.value = ${JSON.stringify(value)};
        node.dispatchEvent(new Event('input', { bubbles: true }));

        return true;
      })()
    `);

    if (!done) {
      throw new Error(`there is no ${locator.css} on the page to type into`);
    }
  }

  async submit(locator) {
    const done = await this.evaluate(`
      (() => {
        const node = document.querySelector(${JSON.stringify(locator.css)});
        const form = node?.closest('form') ?? node;

        if (!form?.requestSubmit) return false;

        form.requestSubmit();

        return true;
      })()
    `);

    if (!done) {
      throw new Error(`there is no form at ${locator.css} to submit`);
    }
  }

  // The box and the computed style of every surface, read in one pass so the page is not asked 51 times.
  async measure(locators, extra = []) {
    const raw = await this.evaluate(`
      (() => {
        const locators = ${JSON.stringify(locators.map((l) => ({ css: l.css, text: l.text ?? null })))};
        const props = ${JSON.stringify([...computedProperties, ...extra])};

        return JSON.stringify(locators.map((loc) => {
          const nodes = [...document.querySelectorAll(loc.css)];
          const node = loc.text ? nodes.find((n) => new RegExp(loc.text).test(n.textContent ?? '')) : nodes[0];

          if (!node) return null;

          const rect = node.getBoundingClientRect();
          const style = getComputedStyle(node);
          const computed = {};

          for (const p of props) computed[p] = style[p] ?? null;

          return {box: {x: rect.x, y: rect.y, width: rect.width, height: rect.height}, computed};
        }));
      })()
    `);

    return JSON.parse(raw);
  }

  // Chrome answers a forced pseudo-class to getComputedStyle exactly as it answers the real one, which is how the
  // probe measures what a pointer over a control, or a control that is disabled, does to it. Nothing in either
  // application disables a beveled button, so the disabled state can only be reached this way.
  async forcePseudoClasses(locators) {
    if (!locators.length) {
      return;
    }

    await this.send('DOM.enable');
    await this.send('CSS.enable');

    const doc = await this.send('DOM.getDocument', { depth: -1 });
    const root = doc?.result?.root;

    if (!root) {
      throw new Error('the browser answered the document request with no document');
    }

    for (const loc of locators) {
      const found = await this.send('DOM.querySelector', { nodeId: root.nodeId, selector: loc.css });

      if (!found?.result?.nodeId) {
        continue;
      }

      const forced = await this.send('CSS.forcePseudoState', {
        nodeId: found.result.nodeId,
        forcedPseudoClasses: loc.force ?? [],
      });

      if (forced?.error) {
        throw new Error(
          `the browser refused to force ${loc.force?.join(', ') ?? 'nothing'} on ${loc.css}: ${forced.error.message}`,
        );
      }
    }
  }

  async screenshot(path) {
    const shot = await this.send('Page.captureScreenshot', { format: 'png' });
    const data = shot?.result?.data;

    if (!data) {
      throw new Error('the browser answered the screenshot with nothing');
    }

    writeFileSync(path, Buffer.from(data, 'base64'));
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

  async request(method, path, { body, form, expect } = {}) {
    const headers = {};

    if (this.token) {
      headers.Authorization = this.token;
    }

    const res = await fetch(this.base + path, {
      method,
      headers,
      body: form ? form : body === undefined ? undefined : JSON.stringify(body),
    });

    const text = await res.text();

    if (expect && !expect.includes(res.status)) {
      throw new Error(`${method} ${path} answered ${res.status}, want ${expect.join(' or ')}: ${text.slice(0, 200)}`);
    }

    return { status: res.status, body: text };
  }

  async login(password) {
    const res = await this.request('POST', '/api/admin/login', { body: { password }, expect: [200] });

    this.token = JSON.parse(res.body).token;

    return this.token;
  }

  async saveConfig(config) {
    await this.request('PUT', '/api/admin/config', { body: config, expect: [200] });
  }

  // A state that changes the configuration has to wait for the server to be holding the new one, because the
  // scanner session reads whatever the server is holding at the moment it connects. Saving an access code and
  // then clearing it, with a reload in between, is otherwise a race between the two saves and the connection.
  async awaitConfig(what, match) {
    for (let attempt = 0; attempt < 40; attempt++) {
      const res = await this.request('GET', '/api/admin/config', { expect: [200] });

      // The configuration is nested: the endpoint answers with the configuration and the password flag beside it.
      if (match(JSON.parse(res.body).config)) return;

      await sleep(250);
    }

    throw new Error(`the server never held the configuration with ${what}`);
  }

  // One recording, posted the way a recording tool posts it, with a fixed timestamp so both servers hold the
  // same call.
  async uploadCall() {
    const form = new FormData();

    for (const [name, value] of Object.entries(callFields())) {
      if (Buffer.isBuffer(value)) {
        form.append(name, new Blob([value], { type: 'audio/wav' }), 'capture.wav');
      } else if (typeof value === 'object') {
        form.append(name, JSON.stringify(value));
      } else {
        form.append(name, String(value));
      }
    }

    await this.request('POST', '/api/call-upload', { form, expect: [200] });
  }
}

// A second of 8 kHz mono 16-bit PCM: a browser only plays audio it can decode, and the display is only populated
// by a call the server really accepted.
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
  view.setUint32(24, rate);
  view.setUint32(28, rate * 2);
  view.setUint16(32, 2);
  view.setUint16(34, 16);
  write(36, 'data');
  view.setUint32(40, samples * 2);

  for (let i = 0; i < samples; i++) {
    view.setInt16(44 + i * 2, Math.round(Math.sin((2 * Math.PI * 440 * i) / rate) * 8000), true);
  }

  return Buffer.from(bytes);
}

const recording = wavFile();

function callFields() {
  return {
    key: 'harness-apikey-open',
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

// The configuration both servers are given: the conformance fixture, which is known to be accepted whole by this
// server, with only the two things the probe varies — the clock format and whether an access code is stored.
function seedConfig({ time12h = true, access = [] } = {}) {
  const fixture = JSON.parse(readFileSync(new URL('../conformance/fixtures/seed.json', import.meta.url), 'utf8'));

  return {
    ...fixture,
    access,
    options: { ...fixture.options, branding: 'Parity Dispatch', time12hFormat: time12h },
  };
}

// ---------------------------------------------------------------------------
// Driving one application through the states
// ---------------------------------------------------------------------------

class Side {
  constructor(name, origin, browser, seedDir, pidFile, app = 'scanner') {
    this.name = name;
    this.origin = origin;
    this.browser = browser;
    this.seedDir = seedDir;
    this.pidFile = pidFile ?? null;
    this.app = app;
    this.pairs = app === 'admin' ? adminPairs : pairs;
    this.states = app === 'admin' ? adminStates : states;
    this.server = new Server(origin);
    this.page = null;
    this.values = {};
    this.shots = [];
    this.motion = [];
  }

  locator(pair) {
    return this.name === 'legacy' ? pair.legacy : pair.current;
  }

  pair(name) {
    const found = this.pairs.find((p) => p.name === name);

    if (!found) {
      throw new Error(`surfaces.mjs has no ${this.app} pair named ${name}`);
    }

    return found;
  }

  control(name) {
    return this.locator(controls[name]);
  }

  async run(only) {
    const chosen = this.pairs.filter((p) => !only || p.name.includes(only));

    await this.server.login('rdio-scanner');
    await this.server.saveConfig(seedConfig());
    await this.server.awaitConfig('no access code', (config) => (config.access ?? []).length === 0);

    this.page = await this.browser.openPage(this.origin);

    let entered = false;

    for (const state of this.states) {
      const inState = chosen.filter((p) => p.states.includes(state));

      if (!inState.length) {
        continue;
      }

      try {
        // Every state starts from a loaded page, so the load happens even in a run that measures no idle surface.
        if (!entered && state !== this.states[0]) {
          await this.enter(this.states[0]);
        }

        await this.enter(state);
        entered = true;

        // A state is measured once the page has stopped moving: the drawer and the snack bar both animate into
        // place, and a box read halfway through a transition is not a measurement of either application.
        if (state !== 'search-pending') {
          await sleep(600);
        }

        for (const viewport of viewports.filter((v) => inState.some((p) => p.viewports.includes(v.name)))) {
          await this.page.setViewport(viewport);

          // A pair measured with a forced pseudo-class is measured in a pass of its own: forcing hover on a button
          // would otherwise change what that same button measures at rest.
          const resting = inState.filter((p) => !p.force?.length);
          const forced = inState.filter((p) => p.force?.length);

          // A pair may ask for properties the matrix does not read everywhere. They are read for the batch and
          // compared only for the pair that asked for them.
          const measured = await this.page.measure(resting.map((p) => this.locator(p)), extras(resting));

          for (let i = 0; i < resting.length; i++) {
            this.values[key(resting[i].id, state, viewport.name)] = measured[i];
          }

          if (forced.length) {
            const locators = forced.map((p) => ({ ...this.locator(p), force: p.force }));

            await this.page.forcePseudoClasses(locators);

            const held = await this.page.measure(locators, extras(forced));

            await this.page.forcePseudoClasses(locators.map((l) => ({ css: l.css, force: [] })));

            for (let i = 0; i < forced.length; i++) {
              this.values[key(forced[i].id, state, viewport.name)] = held[i];
            }
          }

          const shot = `${this.seedDir}/${this.name}-${slug(state)}-${viewport.name}.png`;

          await this.page.screenshot(shot);
          this.shots.push(shot);
        }
      } finally {
        // Whatever happened while the page was being measured, the server is not left stopped.
        await this.resumeServer();
      }
    }

    return this.values;
  }

  // A drawer that snaps open and a drawer that slides look identical once they are open. Before the press that
  // opens one, the harness installs a sampler in the page that records the drawer's box every frame for the length
  // of the motion, so the trace of the slide is kept as the proof that the motion is there at all.
  async startMotionTrace(name) {
    const css = this.locator(this.pair(name)).css;

    await this.page.evaluate(`(() => {
      window.__rdioMotion = [];

      const started = performance.now();
      const sample = () => {
        const rect = document.querySelector(${JSON.stringify(css)})?.getBoundingClientRect();

        window.__rdioMotion.push({
          t: Math.round(performance.now() - started),
          x: rect ? Math.round(rect.x) : null,
          width: rect ? Math.round(rect.width) : null,
        });

        if (performance.now() - started < 700) requestAnimationFrame(sample);
      };

      requestAnimationFrame(sample);
    })()`);
  }

  async captureMidTransition(state) {
    const name = state === 'select-open' ? 'select drawer' : 'search drawer';
    const shot = `${this.seedDir}/${this.name}-${slug(state)}-midtransition.png`;

    // 150ms into a 400ms slide is a little under halfway along it, which is far enough from both ends to tell a
    // sliding drawer from one that simply appears.
    await sleep(150);
    await this.page.screenshot(shot);
    this.shots.push(shot);

    // The frame is the picture; the trace beside it is the number, and it is read once the motion has finished.
    await sleep(400);

    const trace = await this.page.evaluate('window.__rdioMotion ?? []');

    this.motion.push({ side: this.name, state, pair: name, trace });
  }

  // A wait on printed text is written this way because innerText hands back what the page actually rendered, and
  // the legacy page prints several of these lines in capitals.
  hasText(value) {
    return `(document.body?.innerText ?? '').toUpperCase().includes(${JSON.stringify(value.toUpperCase())})`;
  }

  async enter(state) {
    if (this.app === 'admin') {
      return this.enterAdmin(state);
    }

    const page = this.page;

    switch (state) {
      case 'idle':
        await page.goto(this.origin);
        await page.waitFor(this.hasText('Parity Dispatch'), { what: 'the server branding' });
        // The display has two looks: lit, and idled after the dimmer delay. Both applications reach the idled look
        // on their own clock, so the state waits for it — but a page that never dims is a gap to be measured, not
        // a state the probe is allowed to fail to reach.
        await page.waitFor(`!!document.querySelector(${JSON.stringify(`${this.locator(this.pair('display')).css}.idle`)})`, { timeout: 8000, what: 'the display to fall idle' }).catch(() => false);
        break;

      case 'live-call':
        await page.click(this.control('livefeed'));
        await this.server.uploadCall();
        await page.waitFor(this.hasText('Dispatch 1'), { what: 'the uploaded call on the display' });
        break;

      case 'controls':
        await page.click(this.control('holdTalkgroup'));
        await page.click(this.control('pause'));
        break;

      case 'select-open':
        await this.startMotionTrace('select drawer');
        await page.click(this.control('select'), { settle: 0 });
        await this.captureMidTransition(state);
        await page.waitFor(`!!document.querySelector(${JSON.stringify(this.locator(this.pair('select panel')).css)})`, { what: 'the select panel' });
        break;

      case 'search-open-24h':
        await this.reloadWith({ time12h: false });
        // The legacy drawer does not animate the open that happens the instant its page has loaded, so the press
        // waits for the page to settle and the motion is measured from there.
        await sleep(600);
        await this.startMotionTrace('search drawer');
        await page.click(this.control('search'), { settle: 0 });
        await this.captureMidTransition(state);
        await page.waitFor(`document.querySelectorAll(${JSON.stringify(this.locator(this.pair('table body row')).css)}).length > 0`, { what: 'a page of search results' });
        break;

      case 'search-open-12h':
        await this.reloadWith({ time12h: true });
        await sleep(600);
        await this.startMotionTrace('search drawer');
        await page.click(this.control('search'), { settle: 0 });
        await this.captureMidTransition(state);
        await page.waitFor(`document.querySelectorAll(${JSON.stringify(this.locator(this.pair('table body row')).css)}).length > 0`, { what: 'a page of search results in 12-hour time' });
        break;

      case 'search-pending':
        // The date filter is the field both applications treat as a search request on a plain change event, and
        // both put the bar into its pending state before the answer arrives. The server is held still for the
        // measurement so that the page really is pending when it is measured, and released straight afterwards.
        await this.holdServer();
        await page.evaluate(`
          (() => {
            const input = document.querySelector(${JSON.stringify(this.control('dateFilter').css)});

            if (!input) return false;

            input.value = '2026-10-06T04:00';
            input.dispatchEvent(new Event('change', { bubbles: true }));

            return true;
          })()
        `);
        break;

      case 'unlock':
        // An access code whose expiration has passed is the one refusal both applications show the same way: the
        // server answers "expired" and each client prints the same sentence under the field. A merely wrong code
        // is refused by the legacy client with no message at all, which would leave nothing to compare.
        await this.server.saveConfig(seedConfig({ access: [{ id: 1, ident: 'Parity code', code: 'parity-expired', expiration: '1970-01-01T00:00:01Z', systems: '*' }] }));
        await this.server.awaitConfig('an access code', (config) => (config.access ?? []).length === 1);
        await page.goto(this.origin);
        await page.waitFor(
          this.name === 'legacy'
            ? `!!document.querySelector('.rdio-display .auth.visible')`
            : `!!document.querySelector('rdio-display .auth:not([hidden])')`,
          { what: 'the unlock overlay, shown' },
        );
        await page.type(this.control('unlockInput'), 'parity-expired');
        await page.submit(this.control('unlockInput'));
        await page.waitFor(this.hasText('This unlock code has expired'), { timeout: 20000, what: 'the refused unlock code' });
        break;

      case 'snackbar-help':
        await this.server.saveConfig(seedConfig());
        await this.server.awaitConfig('no access code', (config) => (config.access ?? []).length === 0);
        await page.goto(this.origin);
        // The help control is only in the page when the configuration has reached the application, so waiting for
        // it to be visible is waiting for the configuration. A hidden control is still a control, and clicking one
        // that is hidden does nothing.
        await page.waitFor(
          `(() => { const el = document.querySelector(${JSON.stringify(this.control('help').css)}); return !!el && !el.closest('[hidden]'); })()`,
          { what: 'the help control, shown' },
        );
        await page.click(this.control('help'));
        await page.waitFor(`!!document.querySelector(${JSON.stringify(this.locator(this.pair('snackbar')).css)})`, { what: 'the help snackbar' });
        await this.awaitSingleDigitCountdown(page);
        break;

      case 'snackbar-promo':
        await page.setUserAgent('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1');
        await page.goto(this.origin);
        await page.waitFor(this.hasText('works natively on'), { timeout: 25000, what: 'the native-application promo' });
        await this.awaitSingleDigitCountdown(page);
        break;
    }
  }

  // The administration is driven the way an administrator drives it: log in, then press the section the measurement
  // is about. Both sides are the same application built from two different commits, so every state has to be reached
  // the same way on both.
  async enterAdmin(state) {
    const page = this.page;

    switch (state) {
      case 'login':
        await page.goto(`${this.origin}/admin/`);
        await page.waitFor(`!!document.querySelector('rdio-admin-login input[name="password"]')`, {
          what: 'the administration login form',
        });
        break;

      case 'todos':
        // The seeded server password, the same one the probe logs the API in with.
        await page.type({ css: 'rdio-admin-login input[name="password"]' }, 'rdio-scanner');
        await page.submit({ css: 'rdio-admin-login input[name="password"]' });
        await page.waitFor(`!!document.querySelector('.rdio-admin-nav [data-section="todos"]')`, {
          timeout: 20000,
          what: 'the administration navigation, after logging in',
        });
        break;

      case 'options':
        await page.click({ css: '.rdio-admin-nav [data-section="options"]' });
        await page.waitFor('!!document.querySelector("rdio-admin-options rdio-toggle")', {
          what: 'the options section and its toggles',
        });
        break;

      case 'logs':
        await page.click({ css: '.rdio-admin-nav [data-section="logs"]' });
        await page.waitFor('!!document.querySelector("rdio-admin-logs rdio-paginator")', {
          what: 'the logs section and its paginator',
        });
        break;

      case 'snackbar':
        // The password section is the administration's own way of putting a message on the screen without changing
        // anything: a change the server refuses prints "Unable to change password" in the snack bar, and that snack
        // bar is the box pair 17 measures.
        await page.click({ css: '.rdio-admin-nav [data-section="password"]' });
        await page.waitFor('!!document.querySelector("rdio-admin-password button.save")', {
          what: 'the password section',
        });
        await page.type({ css: 'rdio-admin-password input[name="currentPassword"]' }, 'parity-wrong-password');
        await page.type({ css: 'rdio-admin-password input[name="newPassword"]' }, 'parity-new-password');
        await page.type({ css: 'rdio-admin-password input[name="verifyNewPassword"]' }, 'parity-new-password');
        await page.click({ css: 'rdio-admin-password button.save' });
        await page.waitFor('!!document.querySelector("rdio-snackbar:not([hidden])")', {
          timeout: 20000,
          what: 'the administration snack bar',
        });
        break;
    }
  }

  // The countdown begins at "10", and its box hugs whatever it prints. Roboto's digits all share one advance, so the
  // box is the same for every single digit and a whole character wider for the first second: the probe waits for that
  // second to pass so that both applications are measured printing one digit.
  async awaitSingleDigitCountdown(page) {
    await page.waitFor(
      `(() => { const el = document.querySelector('.countdown'); return !!el && el.textContent.trim().length === 1; })()`,
      { timeout: 15000, what: 'the countdown printing a single digit' },
    );
  }

  async reloadWith(options) {
    await this.server.saveConfig(seedConfig(options));
    await this.server.awaitConfig('the clock format the harness saved', (config) => Boolean(config.options?.time12hFormat) === options.time12h);
    await this.page.goto(this.origin);
    await this.page.waitFor(this.hasText('Parity Dispatch'), { what: 'the reloaded page' });
  }

  // A page of results travels over the listener's websocket, and a localhost answer arrives faster than any
  // measurement can be taken. Stopping the server for a moment is how the probe gets to see a page that is
  // genuinely still pending: the request is in flight, the client is waiting, and nothing about either page is
  // faked. The pid file is written by run.sh.
  async holdServer() {
    if (!this.pidFile) return false;

    const pid = Number(readFileSync(this.pidFile, 'utf8').trim());

    if (!pid) return false;

    process.kill(pid, 'SIGSTOP');
    this.held = pid;

    return true;
  }

  async resumeServer() {
    if (!this.held) return;

    process.kill(this.held, 'SIGCONT');
    this.held = null;
  }
}

// ---------------------------------------------------------------------------
// Comparing one measured surface against the other
// ---------------------------------------------------------------------------

const pxProperties = new Set([
  'padding-top', 'padding-right', 'padding-bottom', 'padding-left',
  'margin-top', 'margin-right', 'margin-bottom', 'margin-left',
  'border-top-width', 'border-right-width', 'border-bottom-width', 'border-left-width',
  'border-top-left-radius', 'border-top-right-radius', 'border-bottom-left-radius', 'border-bottom-right-radius',
  'font-size', 'line-height', 'letter-spacing',
]);

const durationProperties = new Set(['transition-duration', 'animation-duration']);

function number(value) {
  if (typeof value === 'number') return value;
  if (typeof value !== 'string') return null;

  const found = /-?\d+(?:\.\d+)?/.exec(value);

  return found ? Number(found[0]) : null;
}

function milliseconds(value) {
  if (typeof value !== 'string') return null;

  return value.endsWith('ms') ? Number(value.slice(0, -2)) : value.endsWith('s') ? Number(value.slice(0, -1)) * 1000 : null;
}

function firstFont(value) {
  return typeof value === 'string' ? value.split(',')[0].trim().replace(/["']/g, '') : value;
}

// A box-shadow is compared layer by layer: the same number of layers, each layer's offsets and color matching and
// each blur within its own tolerance. Two shadows that differ only in how Material spells the same elevation are
// still two different shadows, and the matrix says so.
function shadowLayers(value) {
  if (!value || value === 'none') return [];

  return value.split(/,(?![^(]*\))/).map((layer) => {
    const inset = /inset/.test(layer);
    const numbers = [...layer.matchAll(/-?\d+(?:\.\d+)?(?:px|em|rem)?/g)].map((m) => number(m[0]));
    const color = /rgba?\([^)]*\)/.exec(layer)?.[0] ?? null;

    return { inset, numbers, color };
  });
}

// The properties a batch of pairs asks to be read on top of the ones the matrix reads everywhere.
function extras(pairs) {
  return [...new Set(pairs.flatMap((p) => p.properties ?? []))];
}

function compareValues(legacy, current, pair) {
  const differences = [];

  for (const [name, a, b] of [
    ['box.x', legacy.box.x, current.box.x],
    ['box.y', legacy.box.y, current.box.y],
    ['box.width', legacy.box.width, current.box.width],
    ['box.height', legacy.box.height, current.box.height],
  ]) {
    if (Math.abs(a - b) > tolerance.px) {
      differences.push({ property: name, legacy: round(a), current: round(b), delta: round(b - a) });
    }
  }

  for (const property of [...computedProperties, ...(pair.properties ?? [])]) {
    const a = legacy.computed[property];
    const b = current.computed[property];

    if (a === b) continue;
    if (a == null && b == null) continue;
    if (a === '' || b === '') continue;
    if (a === 'none' && b === 'none') continue;
    if (a === 'auto' && b === 'auto') continue;

    if (pxProperties.has(property)) {
      const na = number(a);
      const nb = number(b);

      if (na === null || nb === null) {
        if (String(a) !== String(b)) differences.push({ property, legacy: a, current: b });
        continue;
      }

      const slack = property === 'line-height' ? tolerance.lineHeight : property === 'letter-spacing' ? tolerance.letterSpacing : tolerance.px;

      if (Math.abs(na - nb) > slack) {
        differences.push({ property, legacy: a, current: b, delta: round(nb - na) });
      }

      continue;
    }

    if (durationProperties.has(property)) {
      const na = milliseconds(a);
      const nb = milliseconds(b);

      if (na === null || nb === null || Math.abs(na - nb) > tolerance.duration) {
        differences.push({ property, legacy: a, current: b });
      }

      continue;
    }

    if (property === 'box-shadow') {
      const la = shadowLayers(a);
      const lb = shadowLayers(b);

      if (la.length !== lb.length) {
        differences.push({ property, legacy: a ?? 'none', current: b ?? 'none', delta: `${la.length} layer(s) against ${lb.length}` });
        continue;
      }

      const bad = la.some((layer, i) => {
        if (layer.inset !== lb[i].inset) return true;
        if ((layer.color ?? '') !== (lb[i].color ?? '')) return true;

        return layer.numbers.some((n, j) => {
          const other = lb[i].numbers[j] ?? 0;
          const slack = j === 2 ? tolerance.blur : tolerance.px;

          return Math.abs(n - other) > slack;
        });
      });

      if (bad) differences.push({ property, legacy: a, current: b });

      continue;
    }

    if (property === 'font-family') {
      if (firstFont(a) !== firstFont(b)) differences.push({ property, legacy: a, current: b });
      continue;
    }

    differences.push({ property, legacy: a, current: b });
  }

  return differences;
}

function round(v) {
  return Math.round(v * 100) / 100;
}

function key(id, state, viewport) {
  return `${id}|${state}|${viewport}`;
}

function slug(state) {
  return state.replace(/[^a-z0-9]+/gi, '-').toLowerCase();
}

// ---------------------------------------------------------------------------
// The matrix
// ---------------------------------------------------------------------------

function buildMatrix(legacyValues, currentValues, chosen) {
  const rows = [];
  const broken = [];

  for (const pair of chosen) {
    for (const state of pair.states) {
      for (const viewport of pair.viewports) {
        const a = legacyValues[key(pair.id, state, viewport)];
        const b = currentValues[key(pair.id, state, viewport)];

        const absent = [a == null ? 'legacy' : null, b == null ? 'current' : null].filter(Boolean);

        // A surface that is on one page and not the other is the gap the feature exists to find. A surface that is
        // on neither page means the selector or the state is wrong, and that is the harness's fault.
        if (absent.length === 2) {
          broken.push({ pair, state, viewport });

          continue;
        }

        if (absent.length === 1) {
          rows.push({
            pair,
            state,
            viewport,
            differences: [{
              property: `absent on the ${absent[0]} side`,
              legacy: a == null ? 'not in the page' : 'measured',
              current: b == null ? 'not in the page' : 'measured',
            }],
          });

          continue;
        }

        rows.push({ pair, state, viewport, differences: compareValues(a, b, pair) });
      }
    }
  }

  return { rows, broken };
}

function writeMatrix(path, rows, { title = 'Parity matrix', sides = ['Legacy', 'Current'] } = {}) {
  const matched = rows.filter((r) => r.differences.length === 0);
  const gaps = rows.filter((r) => r.differences.length > 0);

  const lines = [
    `# ${title}`,
    '',
    `Measured rows: ${rows.length}. Matching: ${matched.length}. Gaps: ${gaps.length}.`,
    '',
    'A row is a surface, a state, and a viewport. The values behind every row are in `values.json` beside this file.',
    '',
    '| # | Surface | State | Viewport | Verdict | Differing properties |',
    '| --- | --- | --- | --- | --- | --- |',
  ];

  for (const row of rows) {
    lines.push(
      `| ${row.pair.id} | ${row.pair.name} | ${row.state} | ${row.viewport} | ${row.differences.length ? '**gap**' : 'match'} | ${
        row.differences.length ? row.differences.map((d) => `\`${d.property}\``).join(' ') : ''
      } |`,
    );
  }

  lines.push('', '## The gaps, property by property', '');

  for (const row of gaps) {
    lines.push(`### ${row.pair.id} ${row.pair.name} — ${row.state} at ${row.viewport}`, '');
    lines.push(`| Property | ${sides[0]} | ${sides[1]} | Delta |`, '| --- | --- | --- | --- |');

    for (const d of row.differences) {
      lines.push(`| \`${d.property}\` | ${cell(d.legacy)} | ${cell(d.current)} | ${d.delta ?? ''} |`);
    }

    lines.push('');
  }

  writeFileSync(path, lines.join('\n'));
}

function cell(value) {
  const text = String(value ?? '').replace(/\|/g, '\\|').replace(/\n/g, ' ');

  return text.length > 120 ? `${text.slice(0, 117)}...` : text;
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

function parseArgs(argv) {
  const args = {};

  for (let i = 0; i < argv.length; i++) {
    if (argv[i].startsWith('--')) {
      args[argv[i].slice(2)] = argv[i + 1]?.startsWith('--') ? true : argv[++i];
    }
  }

  return args;
}

async function sleep(ms) {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

const args = parseArgs(process.argv.slice(2));

for (const needed of ['current', 'legacy', 'out']) {
  if (!args[needed]) {
    console.error(`parity.mjs: --${needed} is required`);

    process.exit(2);
  }
}

const out = args.out;
const only = args.only ?? null;
// The probe measures two things with the same machinery: the scanner page against the baseline it is meant to
// match, and the administration page against itself, built from before the work and from after it.
const app = args.app === 'admin' ? 'admin' : 'scanner';
const chosen = (app === 'admin' ? adminPairs : pairs).filter((p) => !only || p.name.includes(only));

mkdirSync(out, { recursive: true });

const browser = await new Browser(Number(args.cdpPort ?? 9444), `${out}/profile`).launch();

let status = 0;

try {
  const legacy = new Side('legacy', args.legacy, browser, out, args.legacyPid, app);
  const current = new Side('current', args.current, browser, out, args.currentPid, app);

  const legacyValues = await legacy.run(only);
  console.log(`parity.mjs: measured ${Object.keys(legacyValues).length} legacy values`);

  const currentValues = await current.run(only);
  console.log(`parity.mjs: measured ${Object.keys(currentValues).length} current values`);

  const { rows, broken } = buildMatrix(legacyValues, currentValues, chosen);

  writeFileSync(`${out}/values.json`, JSON.stringify({ legacy: legacyValues, current: currentValues }, null, 2));
  writeFileSync(`${out}/motion.json`, JSON.stringify([...legacy.motion, ...current.motion], null, 2));
  writeMatrix(
    `${out}/matrix.md`,
    rows,
    app === 'admin'
      ? { title: 'Administration regression matrix', sides: ['Before', 'After'] }
      : {},
  );

  for (const frame of [...legacy.motion, ...current.motion]) {
    const samples = frame.trace.filter((s) => s.x !== null);
    const near = (ms) =>
      samples.reduce((best, s) => (Math.abs(s.t - ms) < Math.abs(best.t - ms) ? s : best), samples[0]);

    console.log(
      `parity.mjs: ${frame.side} ${frame.pair} during ${frame.state}: ` +
        `x=${samples[0]?.x ?? 'none'} at the press, ` +
        `${near(150)?.x ?? 'none'} at 150ms, ` +
        `${samples[samples.length - 1]?.x ?? 'none'} settled`,
    );
  }

  const gaps = rows.filter((r) => r.differences.length);

  console.log(`parity.mjs: ${rows.length} rows, ${rows.length - gaps.length} matching, ${gaps.length} gaps`);

  if (broken.length) {
    console.error(`parity.mjs: ${broken.length} surface(s) resolved on neither side, which is a harness fault:`);

    for (const m of broken.slice(0, 20)) {
      console.error(`  - pair ${m.pair.id} ${m.pair.name} in ${m.state} at ${m.viewport}`);
    }

    status = 2;
  } else {
    status = gaps.length ? 1 : 0;
  }
} catch (error) {
  console.error(`parity.mjs: ${error.message ?? error}`);

  status = 2;
} finally {
  await browser.close();
}

process.exit(status);
