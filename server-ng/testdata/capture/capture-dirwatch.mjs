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

// capture-dirwatch.mjs is the listener side of the end-to-end capture capture-dirwatch.sh runs. It asks
// for the configuration, subscribes to the talkgroups that configuration names, and only then lets the
// recordings arrive — because a server broadcasts a call only to a client that asked for that talkgroup.
//
// The recordings are written by this script, into the directories the server is watching, one at a time:
// the second is dropped only after the first has been broadcast, so that the frames arrive in an order
// both servers can be expected to produce. They are the same recordings the server-ng test writes, byte
// for byte, which is why the golden can pin how long they are.
//
// What is recorded is every frame the server sent, in the order it sent it, with the recording inside a
// call frame replaced by how many bytes it was: the recording is the file the capture wrote itself, and
// what the golden has to pin is that the server passed it along whole.

import fs from 'node:fs';
import path from 'node:path';

const args = {};

for (let i = 2; i < process.argv.length; i += 2) {
  args[process.argv[i]] = process.argv[i + 1];
}

for (const name of ['--url', '--out', '--watch-dsd', '--watch-trunk']) {
  if (!args[name]) {
    console.error(`capture-dirwatch.mjs: ${name} is required`);

    process.exit(2);
  }
}

// dsdRecordingName is a recording the way dsd+ names one: the clock first, then the talkgroup, then the
// system as a net and a WACN, then the talkgroup again with its label, then the radio with its label. The
// clock it names is a local time, and the server's time zone is what turns it into a moment — which is why
// capture-dirwatch.sh runs the server with TZ=UTC.
const dsdRecordingName = '120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3';

// trunkRecorderSidecar is the JSON a trunk-recorder instance writes beside its recording, and it is the
// only way that recording says what it is.
const trunkRecorderSidecar = `{
  "short_name": "Grand Metro",
  "talkgroup": 1514000,
  "talkgroup_description": "Dispatch 1",
  "talkgroup_group": "Ground",
  "talkgroup_group_tag": "Dispatch",
  "talkgroup_tag": "Main",
  "timestamp": 1791288000000
}
`;

// recording is audio for a watched file. Nothing in this run looks inside the recording — a watch reads a
// recording's name and its sidecar, and the ingest path only asks whether there is more audio than a
// container header — so what matters is that it is as long as the recording the server-ng test writes.
const recording = (bytes) => Buffer.from(Array.from({ length: bytes }, (_, i) => i % 251));

// The references the listener subscribes to are the ones the seeded configuration names: the systems are
// 10001 and 4660, and both have a talkgroup 1514000.
const livefeed = {
  10001: { 1514000: true },
  4660: { 1514000: true },
};

const frames = [];

let subscribed = false;
let dropped = 0;

const finish = () => {
  fs.writeFileSync(args['--out'], JSON.stringify(frames, null, 2) + '\n');

  console.log('capture-dirwatch.mjs: frames captured:', frames.length);

  process.exit(0);
};

const readFrame = (text) => {
  const message = JSON.parse(text);

  if (!Array.isArray(message)) {
    return null;
  }

  const [command, payload] = message;

  if (command === 'CAL' && payload && payload.audio) {
    const { audio, ...rest } = payload;

    return {
      command,
      payload: {
        ...rest,
        audioBytes: audio.data.length,
      },
    };
  }

  // The configuration a listener is sent is the whole configuration, and it is pinned by the configuration
  // goldens rather than being repeated here.
  if (command === 'CFG') {
    return { command };
  }

  return { command, payload };
};

const c = new WebSocket(args['--url']);

c.onopen = () => c.send(JSON.stringify(['CFG']));

c.onmessage = (e) => {
  let frame = null;

  try {
    frame = readFrame(String(e.data));
  } catch (err) {
    console.log('capture-dirwatch.mjs: a frame is not a message:', err.message);

    return;
  }

  if (frame) {
    frames.push(frame);
  }

  const command = frame?.command;

  if (!subscribed && command === 'CFG') {
    subscribed = true;

    c.send(JSON.stringify(['LFM', livefeed]));

    return;
  }

  if (command === 'LFM' && dropped === 0) {
    dropped = 1;

    fs.writeFileSync(path.join(args['--watch-trunk'], 'call.wav'), recording(2092));
    fs.writeFileSync(path.join(args['--watch-trunk'], 'call.json'), trunkRecorderSidecar);

    console.log('capture-dirwatch.mjs: the trunk-recorder pair was dropped');

    return;
  }

  if (command === 'CAL' && dropped === 1) {
    dropped = 2;

    fs.writeFileSync(path.join(args['--watch-dsd'], dsdRecordingName), recording(2048));

    console.log('capture-dirwatch.mjs: the dsd+ recording was dropped');

    return;
  }

  if (command === 'CAL' && dropped === 2) {
    dropped = 3;

    // The listener-count report is written five seconds after the count changes, and it counts the
    // listeners that are there when it is written. Staying connected past that moment is what puts the
    // count the capture recorded into the log.
    setTimeout(finish, 8000);
  }
};

c.onerror = (e) => console.log('capture-dirwatch.mjs: socket error:', e.message ?? e.error);

setTimeout(finish, 40000);
