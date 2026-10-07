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
// along with this program.  If not, see <http://www.gnu.org/licenses/>
//
// WebSocket API Access Policy:
// This WebSocket API is reserved exclusively for Saubeo Solutions and its native applications.
// Unauthorized access is strictly prohibited.
// See API_ACCESS_POLICY.md for full terms.

// differential.mjs drives one scenario against one server and writes down what that server did.
//
// It is run twice per scenario, once against the legacy binary and once against server-ng, with the same
// arguments apart from the address and the directories. What it writes is a transcript of the steps it took
// and the answers it got, in the order they happened, and compare.mjs is what decides whether the two
// transcripts agree.
//
// Nothing here knows what the answer is supposed to be. A scenario that expects a frame and does not get one
// is a harness failure, and it stops the run; a scenario that gets two different answers from the two servers
// is a divergence, and compare.mjs decides whether it is one of the approved ones.

import fs from 'node:fs';
import path from 'node:path';

const adminPassword = 'rdio-scanner';

const openKey = 'harness-apikey-open';
const scopedKey = 'harness-apikey-scoped';

// callMoment is the moment every uploaded recording in this harness names. It is a fixed moment rather than
// "now" so that the call a listener is sent, the row the server stores, and the search a listener runs all
// carry something both servers must produce identically.
const callMoment = 1791288000000;

// recording is audio for an uploaded call. Nothing in this harness looks inside it: the ingest path asks only
// whether there is more audio than a container header, and the point of the comparison is that the server
// passed the same bytes along.
const recording = Buffer.from(Array.from({ length: 2048 }, (_, i) => i % 251));

// The two recordings the watched directories receive are the two a recording tool actually writes: a
// trunk-recorder pair, where a sidecar beside the audio says what the call is, and a dsd+ recording whose
// whole description is its file name.
const dsdRecordingName = '120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3';

const dsdDayDirectory = '20261006';

const trunkRecorderSidecar = `{
  "short_name": "Grand Metro",
  "talkgroup": 1514000,
  "talkgroup_description": "Dispatch 1",
  "talkgroup_group": "Ground",
  "talkgroup_group_tag": "Dispatch",
  "talkgroup_tag": "Main",
  "timestamp": ${callMoment}
}
`;

// Each scenario is run on a database of its own, and the configuration it is seeded with is the shared
// fixture with these adjustments applied. showListenersCount is turned off wherever a scenario connects more
// than one listener or does not care about the count report, because the count is broadcast on a debounce and
// a debounced report is a log row whose number depends on how the connections happened to land.
//
// sortTalkgroups is off in the fixture itself: the legacy server's sort renumbers the talkgroup orders it
// holds, so a second listener in the same run is told numbers the first listener's configuration invented
// (Q60), which is not a divergence this harness is set up to expect.
const scenarioSetup = {
  'listener-frames': { showListenersCount: true },
  'missing-call-fetch': { showListenersCount: false },
  'admin-rest': {},
  'admin-config-socket': {},
  'scoped-upload': {},
  'access-limit': { showListenersCount: false },
  'scope-normalization': {},
  'dirwatch-calls': { showListenersCount: false, dirwatches: true },
  'cross-write': { showListenersCount: false },
  'cross-read': { showListenersCount: false },
};

// ---------------------------------------------------------------------------

class Run {
  constructor(args, setup) {
    this.args = args;
    this.setup = setup;
    this.transcript = new Transcript(args.scenario, args.out);

    this.base = args.base;
    this.wsBase = args.base.replace(/^http:/, 'ws:');

    this.token = '';
  }

  // seed applies the shared configuration through the administrator API, which is the way an administrator
  // applies a configuration and the way that does not depend on either server's database schema being
  // understood by the harness.
  //
  // The seed is not recorded: it is the arrangement the scenario is run on, and the same paths are compared
  // in the scenarios that read and save the configuration.
  async seed() {
    const config = structuredClone(seedFixture);

    config.options = { ...config.options, ...this.setup };

    if (this.setup.dirwatches) {
      config.dirwatch = [
        {
          id: 0,
          delay: 2000,
          deleteAfter: true,
          directory: this.args['watch-dsd'],
          disabled: false,
          extension: '',
          frequency: 0,
          mask: '',
          order: 1,
          siteId: 0,
          systemId: 0,
          talkgroupId: 0,
          type: 'dsdplus',
        },
        {
          id: 0,
          delay: 2000,
          deleteAfter: true,
          directory: this.args['watch-trunk'],
          disabled: false,
          extension: '',
          frequency: 0,
          mask: '',
          order: 2,
          siteId: 0,
          systemId: 0,
          talkgroupId: 0,
          type: 'trunk-recorder',
        },
      ];
    }

    const login = await this.call('POST', '/api/admin/login', { body: { password: adminPassword } });

    if (login.status !== 200) {
      throw new Error(`the administrator login did not work: ${JSON.stringify(login)}`);
    }

    this.token = JSON.parse(login.body).token;

    const saved = await this.call('PUT', '/api/admin/config', { token: this.token, body: config });

    if (saved.status !== 200) {
      throw new Error(`the configuration was not applied: ${saved.status} ${saved.body}`);
    }
  }

  // request makes one http call and hands it back, including the time it took: some of what this harness
  // compares is what a server does, and one of them is how long it takes to do it. The scenario decides
  // whether the answer is part of what gets compared.
  async request(method, route, { name, token, body, form } = {}) {
    const step = await this.call(method, route, { token, body, form });

    step.name = name;

    return step;
  }

  // call is the same http call, not recorded. The seed is the arrangement a scenario is run on, and the paths
  // it uses are compared in the scenarios that read and save the configuration.
  async call(method, route, { token, body, form } = {}) {
    const headers = {};

    if (token) {
      headers['Authorization'] = token;
    }

    const started = Date.now();

    try {
      const res = await fetch(this.base + route, {
        method,
        headers,
        body: form ?? (body === undefined ? undefined : JSON.stringify(body)),
      });

      const text = await res.text();

      const step = { kind: 'http', method, route, status: res.status, body: text, elapsedMs: Date.now() - started };

      return step;
    } catch (err) {
      // A server that answers nothing at all is an answer, and it is recorded as one: the legacy server ends
      // a request that panics with no reply, and that is the difference the harness has to be able to see.
      const step = {
        kind: 'http',
        method,
        route,
        error: String(err?.cause?.code ?? err?.cause ?? err.message),
        elapsedMs: Date.now() - started,
      };

      return step;
    }
  }

  // upload posts one recording to one of the upload endpoints, with the fields an uploader actually posts.
  async upload(route, fields, name) {
    const form = new FormData();

    for (const [name, value] of Object.entries(fields)) {
      if (Buffer.isBuffer(value)) {
        form.append(name, new Blob([value], { type: 'audio/wav' }), 'capture.wav');
      } else if (typeof value === 'object') {
        form.append(name, JSON.stringify(value));
      } else {
        form.append(name, String(value));
      }
    }

    return this.request('POST', route, { name, form });
  }

  async connect(route = '/') {
    const session = new Session(this.wsBase + route);

    await session.opened;

    return session;
  }

  // listen is one listener connection, authenticated with an access code, asked for its configuration.
  async listen(step, code) {
    const session = await this.connect();

    if (code) {
      session.send(['PIN', Buffer.from(code).toString('base64')]);
    } else {
      session.send(['CFG']);
    }

    const frame = await session.expect(step);

    this.transcript.record(frame);

    return { session, frame };
  }

  record(step) {
    this.transcript.record(step);
  }

  sleep(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms));
  }
}

// Session is one websocket connection and the frames it received, kept exactly as they arrived.
class Session {
  constructor(url) {
    this.queue = [];
    this.waiter = null;

    this.conn = new WebSocket(url);

    this.conn.addEventListener('message', (event) => {
      const frame = String(event.data);

      if (this.waiter) {
        const waiting = this.waiter;

        this.waiter = null;

        waiting(frame);

        return;
      }

      this.queue.push(frame);
    });

    this.opened = new Promise((resolve, reject) => {
      this.conn.addEventListener('open', resolve, { once: true });
      this.conn.addEventListener('error', (event) => reject(new Error(event.message ?? 'websocket error')), { once: true });
    });

    this.closed = new Promise((resolve) => this.conn.addEventListener('close', resolve));
  }

  send(message) {
    this.conn.send(JSON.stringify(message));
  }

  // sendRaw sends exactly the text it is given, which is what an administrator socket expects: a credential,
  // and not a json document holding one.
  sendRaw(text) {
    this.conn.send(text);
  }

  // expect waits for the next frame this server sends, and records it as it arrived.
  async expect(step, { timeout = 12000 } = {}) {
    const frame = await this.waitForFrame(timeout);

    if (frame === null) {
      throw new Error(`no frame arrived for ${step} within ${timeout}ms`);
    }

    const parsed = JSON.parse(frame);

    return { step, kind: 'frame', command: Array.isArray(parsed) ? parsed[0] : null, frame };
  }

  // expectNothing waits and proves that nothing arrived, which is how a request the server answers with
  // silence is recorded.
  async expectNothing(step, { timeout = 3000 } = {}) {
    const frame = await this.waitForFrame(timeout);

    if (frame !== null) {
      throw new Error(`${step}: expected nothing, got ${frame.slice(0, 80)}`);
    }

    return { step, kind: 'frame', absent: true };
  }

  waitForFrame(timeout) {
    if (this.queue.length > 0) {
      return Promise.resolve(this.queue.shift());
    }

    return new Promise((resolve) => {
      const timer = setTimeout(() => {
        this.waiter = null;

        resolve(null);
      }, timeout);

      this.waiter = (frame) => {
        clearTimeout(timer);

        resolve(frame);
      };
    });
  }

  async close() {
    this.conn.close();

    await this.closed;
  }
}

class Transcript {
  constructor(scenario, out) {
    this.scenario = scenario;
    this.out = out;
    this.steps = [];
  }

  record(step) {
    this.steps.push(step);
  }

  write() {
    const out = { scenario: this.scenario, steps: this.steps };

    fs.writeFileSync(this.out, JSON.stringify(out, null, 2) + '\n');
  }
}

// ---------------------------------------------------------------------------
// The scenarios.

const scenarios = {
  // listener-frames is the listener's whole conversation: the configuration, the livefeed subscription, a call
  // arriving from an upload, the count report, the call asked for by id, and the call search.
  'listener-frames': async (run) => {
    await run.seed();

    const listener = await run.listen('configuration-is-sent');

    listener.session.send(['LFM', { 10001: { 1514000: true }, 4660: { 1514000: true } }]);

    run.record(await listener.session.expect('livefeed-is-acknowledged'));

    await run.upload('/api/call-upload', uploadFields(openKey), 'a-call-is-uploaded');

    const live = await listener.session.expect('a-subscribed-call-arrives');

    const callId = JSON.parse(live.frame)[1].id;

    run.record(await listener.session.expect('the-listener-count-is-reported'));

    listener.session.send(['CAL', String(callId), 'p']);

    run.record(await listener.session.expect('the-call-is-fetched'));

    listener.session.send(['LCL', { limit: 200, offset: 0, sort: 'date' }]);

    run.record(await listener.session.expect('the-calls-are-searched'));

    await listener.session.close();
  },

  // missing-call-fetch is quirk Q56: the legacy server leaves its database transaction open when the call it
  // was asked for is not there, and on SQLite the next write waits out the busy timeout. The content of the
  // answer is the same in both servers — there is no answer — so what the harness measures is the write that
  // follows it.
  'missing-call-fetch': async (run) => {
    await run.seed();

    const listener = await run.listen('configuration-is-sent');

    listener.session.send(['CAL', '999999', 'p']);

    run.record(await listener.session.expectNothing('a-call-that-is-not-there-is-not-answered'));

    run.record(
      await run.request('POST', '/api/admin/login', {
        name: 'a-write-after-the-missing-call-fetch',
        body: { password: 'not-the-password' },
      }),
    );

    await listener.session.close();
  },

  // admin-rest is the administrator's http surface: the whole configuration, each section on its own, the log
  // search, the alerts, a section saved, an access added, and the session ended.
  'admin-rest': async (run) => {
    await run.seed();

    run.record(
      await run.request('POST', '/api/admin/login', { name: 'the-administrator-logs-in', body: { password: adminPassword } }),
    );

    run.record(await run.request('GET', '/api/admin/config', { name: 'the-whole-configuration', token: run.token }));

    for (const section of ['access', 'apikeys', 'dirwatch', 'downstreams', 'groups', 'options', 'systems', 'tags']) {
      run.record(await run.request('GET', `/api/admin/config/${section}`, { name: `the-${section}-section`, token: run.token }));
    }

    run.record(
      await run.request('POST', '/api/admin/logs', {
        name: 'the-log-search',
        token: run.token,
        body: { level: 'info', limit: 200, offset: 0, sort: 0 },
      }),
    );

    run.record(await run.request('GET', '/api/admin/alerts', { name: 'the-alerts', token: run.token }));

    run.record(
      await run.request('PUT', '/api/admin/config/options', {
        name: 'the-options-are-saved',
        token: run.token,
        body: { ...seedFixture.options, branding: 'Harness Dispatch, renamed' },
      }),
    );

    run.record(
      await run.request('POST', '/api/admin/user-add', {
        name: 'an-access-is-added',
        token: run.token,
        body: { code: 'harness-added', ident: 'Harness added', limit: 5, systems: '*' },
      }),
    );

    run.record(await run.request('GET', '/api/admin/config/access', { name: 'the-access-section-after-the-add', token: run.token }));

    run.record(await run.request('POST', '/api/admin/logout', { name: 'the-administrator-logs-out', token: run.token }));

    run.record(await run.request('GET', '/api/admin/config', { name: 'the-configuration-after-the-session-is-over', token: run.token }));
  },

  // admin-config-socket is the socket the web application keeps open: a credential presented on the socket, a
  // configuration saved, and the whole configuration pushed back down the socket that asked for nothing.
  'admin-config-socket': async (run) => {
    await run.seed();

    const socket = await run.connect('/api/admin/config');

    socket.sendRaw(run.token);

    // The connection is put into the set of connections to push to by a loop of its own, so the save has to
    // wait for the connection to have been handed over.
    await run.sleep(500);

    // The groups as the server has them, so that this save adds one group instead of tripping the legacy
    // server's rule that a save naming something new keeps every row it already had (Q33) — a save the two
    // servers are known not to agree on, and not what this scenario is for.
    const stored = await run.request('GET', '/api/admin/config/groups', { name: 'the-groups-as-stored', token: run.token });

    run.record(stored);

    run.record(
      await run.request('PUT', '/api/admin/config/groups', {
        name: 'a-group-is-added',
        token: run.token,
        body: [...JSON.parse(stored.body), { id: 0, label: 'Harness added group' }],
      }),
    );

    run.record(await socket.expect('the-configuration-is-pushed'));

    await socket.close();
  },

  // scoped-upload is quirk Q51: the legacy server checks an apikey's scope before the call's system has been
  // resolved, a scoped key dereferences the unresolved system, and the request ends with no reply at all.
  'scoped-upload': async (run) => {
    await run.seed();

    run.record(await run.upload('/api/call-upload', uploadFields(scopedKey), 'a-scoped-key-uploads-a-call'));
  },

  // access-limit is quirk Q62: the legacy server counts the sessions holding an access by comparing the
  // in-memory access object, and saving the access section replaces those objects, so the limit stops counting
  // for the sessions that connected before the save.
  'access-limit': async (run) => {
    await run.seed();

    const limited = [{ id: 0, code: 'harness-limited', ident: 'Harness limited', limit: 1, systems: '*' }];

    run.record(
      await run.request('PUT', '/api/admin/config/access', { name: 'a-limited-access-is-saved', token: run.token, body: limited }),
    );

    const created = await run.request('GET', '/api/admin/config/access', { name: 'the-limited-access', token: run.token });

    run.record(created);

    const accessId = JSON.parse(created.body)[0].id;

    const first = await run.listen('the-first-listener-is-let-in', 'harness-limited');
    const second = await run.listen('the-second-listener-is-let-in', 'harness-limited');

    // The same access, saved again with the identifier the server gave it: an update, and not the addition of
    // a second access with the same code.
    run.record(
      await run.request('PUT', '/api/admin/config/access', {
        name: 'the-same-access-is-saved-again',
        token: run.token,
        body: [{ ...limited[0], id: accessId }],
      }),
    );

    const third = await run.listen('the-third-listener-is-refused', 'harness-limited');

    await first.session.close();
    await second.session.close();
    await third.session.close();
  },

  // scope-normalization is quirk Q61: a scope entry whose talkgroups are named as neither the wildcard nor a
  // list is stored by the legacy server exactly as it was submitted, and by server-ng as the scope it
  // understood.
  'scope-normalization': async (run) => {
    await run.seed();

    const oddScope = [{ id: 0, ident: 'Harness odd scope', code: 'harness-odd', systems: [{ id: 10001, talkgroups: 'Fire' }] }];

    run.record(
      await run.request('PUT', '/api/admin/config/access', { name: 'an-access-with-an-odd-scope-is-saved', token: run.token, body: oddScope }),
    );

    // The apikey the seed created, with its scope replaced by something that is neither the wildcard nor a
    // list. Saving the stored list keeps every entry in it, which is what a save that names nothing new does
    // in both servers.
    const stored = await run.request('GET', '/api/admin/config/apikeys', { name: 'the-apikeys-as-stored', token: run.token });

    run.record(stored);

    const oddKey = JSON.parse(stored.body).map((apikey) =>
      apikey.key === 'harness-apikey-scoped' ? { ...apikey, systems: [{ id: 10001, talkgroups: 7 }] } : apikey,
    );

    run.record(
      await run.request('PUT', '/api/admin/config/apikeys', { name: 'an-apikey-with-an-odd-scope-is-saved', token: run.token, body: oddKey }),
    );

    run.record(await run.request('GET', '/api/admin/config/access', { name: 'the-access-with-the-odd-scope', token: run.token }));

    run.record(await run.request('GET', '/api/admin/config/apikeys', { name: 'the-apikey-with-the-odd-scope', token: run.token }));
  },

  // dirwatch-calls is the path a recording tool's output becomes a call: two watched directories, one
  // recording dropped after the other has been broadcast, and a listener that subscribed before either.
  'dirwatch-calls': async (run) => {
    await run.seed();

    const listener = await run.listen('configuration-is-sent');

    listener.session.send(['LFM', { 10001: { 1514000: true }, 4660: { 1514000: true } }]);

    run.record(await listener.session.expect('the-livefeed-is-acknowledged'));

    // The sidecar before the audio it describes: a watcher that finds the audio first has nothing to name the
    // call with, and a watcher that deletes what it could not read would take the recording with it.
    fs.writeFileSync(path.join(run.args['watch-trunk'], 'call.json'), trunkRecorderSidecar);
    fs.writeFileSync(path.join(run.args['watch-trunk'], 'call.wav'), recording);

    run.record(await listener.session.expect('the-trunk-recorder-pair-becomes-a-call', { timeout: 20000 }));

    // The dsd+ recording lands in the dated directory dsd+ writes into, which is where the moment it is
    // stamped with comes from: the parser reads the day from the directory and the clock from the file name.
    fs.writeFileSync(path.join(run.args['watch-dsd'], dsdDayDirectory, dsdRecordingName), recording);

    run.record(await listener.session.expect('the-dsd-recording-becomes-a-call', { timeout: 20000 }));

    await listener.session.close();
  },

  // cross-write and cross-read are the two halves of the handoff the harness calls T052: one server writes a
  // database, the other one is started on a copy of it and reads it back.
  //
  // cross-write seeds a configuration, ingests a call, and then reads the whole thing back. Only the reading
  // is recorded: what the writing did is visible in the database dump, and what has to match is what the two
  // servers say about the same stored rows.
  'cross-write': async (run) => {
    await run.seed();

    await run.upload('/api/call-upload', uploadFields(openKey), 'a-call-is-uploaded');

    await readBack(run);
  },

  // cross-read is the server that was handed a database it did not write, reading it with nothing seeded and
  // nothing saved. Its login is not recorded, because the writer's login is part of its seed and is not
  // recorded either: what is compared is what each server says about the stored rows.
  'cross-read': async (run) => {
    const login = await run.request('POST', '/api/admin/login', { name: 'the-administrator-logs-in', body: { password: adminPassword } });

    if (login.status !== 200) {
      throw new Error(`the administrator could not log in to a database the other server wrote: ${JSON.stringify(login)}`);
    }

    run.token = login.token ?? JSON.parse(login.body).token;

    await readBack(run);
  },
};

// readBack is the reading half, which both halves of the handoff do identically: the configuration as the
// server has it, the sections an administrator opens, and the calls a listener can find and ask for.
async function readBack(run) {
  run.record(await run.request('GET', '/api/admin/config', { name: 'the-whole-configuration', token: run.token }));

  for (const section of ['access', 'apikeys', 'groups', 'options', 'systems', 'tags']) {
    run.record(await run.request('GET', `/api/admin/config/${section}`, { name: `the-${section}-section`, token: run.token }));
  }

  const listener = await run.listen('the-configuration-is-sent-to-a-listener');

  listener.session.send(['LCL', { limit: 200, offset: 0, sort: 'date' }]);

  const search = await listener.session.expect('the-calls-are-searched');

  run.record(search);

  const callId = JSON.parse(search.frame)[1].results[0].id;

  listener.session.send(['CAL', String(callId), 'p']);

  run.record(await listener.session.expect('the-call-is-fetched'));

  await listener.session.close();
}

// ---------------------------------------------------------------------------

function uploadFields(key) {
  return {
    key,
    system: 10001,
    talkgroup: 1514000,
    talkgroupLabel: 'Ground',
    talkgroupName: 'Dispatch 1',
    timestamp: callMoment,
    site: 101,
    frequencies: [{ dbm: -70, errorCount: 1, freq: 151400000, pos: 0.5, spikeCount: 2 }],
    patches: [1514100],
    sources: [{ pos: 0.5, src: 1001, tag: 'Car 1' }, { pos: 1.5, src: 1002, tag: 'Car 2' }],
    audio: recording,
    audioFilename: 'capture.wav',
    audioMime: 'audio/wav',
  };
}

function parseArgs(argv) {
  const parsed = {};

  for (let i = 0; i < argv.length; i += 2) {
    const name = argv[i];

    if (!name.startsWith('--')) {
      throw new Error(`unknown argument ${name}`);
    }

    parsed[name.slice(2)] = argv[i + 1];
  }

  for (const name of ['scenario', 'base', 'out']) {
    if (!parsed[name]) {
      throw new Error(`--${name} is required`);
    }
  }

  if (scenarioSetup[parsed.scenario]?.dirwatches) {
    for (const name of ['watch-dsd', 'watch-trunk']) {
      if (!parsed[name]) {
        throw new Error(`--${name} is required for the dirwatch scenario`);
      }
    }
  }

  return parsed;
}

// ---------------------------------------------------------------------------
// The run.

const args = parseArgs(process.argv.slice(2));

const seedFixture = JSON.parse(fs.readFileSync(path.join(import.meta.dirname, 'fixtures', 'seed.json'), 'utf8'));

const setup = scenarioSetup[args.scenario];

if (!setup) {
  console.error(`differential.mjs: no scenario named ${args.scenario}`);

  process.exit(2);
}

const run = new Run(args, setup);

try {
  await scenarios[args.scenario](run);
} catch (err) {
  console.error(`differential.mjs: ${args.scenario}: ${err.message}`);

  run.transcript.steps.push({ kind: 'harness-failure', error: err.message });

  run.transcript.write();

  process.exit(1);
}

run.transcript.write();
