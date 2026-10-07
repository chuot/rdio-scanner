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

// fixtures-autopopulate.mjs writes the recordings the auto-populate capture drops, into a staging
// directory. capture-autopopulate.sh copies them out of it one at a time, in the order below, because the
// order is what each recording is testing.
//
//   node fixtures-autopopulate.mjs --out stagedir
//
// The recordings are the same shape the server-ng test writes, byte for byte, which is why the golden can
// pin how long they are. Nothing in the run looks inside a recording: a watch reads a recording's name and
// its sidecar, and the ingest path only asks whether there is more audio than a container header.

import fs from 'node:fs';
import path from 'node:path';

const args = {};

for (let i = 2; i < process.argv.length; i += 2) {
  args[process.argv[i]] = process.argv[i + 1];
}

if (!args['--out']) {
  console.error('fixtures-autopopulate.mjs: --out is required');

  process.exit(2);
}

const recording = (bytes) => Buffer.from(Array.from({ length: bytes }, (_, i) => i % 251));

const sidecar = (fields) => JSON.stringify(fields, null, 2) + '\n';

// The first recording names a system the server has never seen, by label, and a talkgroup it has never
// seen, with nothing beside the reference: no description, no group, no tag. That is the recording that
// makes a talkgroup whose label is the reference written out as a number, which is the thing the second
// recording is there to repair.
const first = {
  short_name: 'Grand Metro',
  talkgroup: 1514000,
  timestamp: 1791288000000,
};

// The second recording is the same system and the same talkgroup, and this time the sidecar says what they
// are called. The server already has the system and the talkgroup, so nothing is created: the placeholder
// label and name are replaced with the ones the sidecar names.
const second = {
  short_name: 'Grand Metro',
  talkgroup: 1514000,
  talkgroup_description: 'Dispatch 1',
  talkgroup_group: 'Ground',
  talkgroup_group_tag: 'Dispatch',
  talkgroup_tag: 'Main',
  timestamp: 1791288060000,
};

// The third recording names a system, a group and a tag the server has never seen, so that the run covers
// the two things a talkgroup is filed under as well as the talkgroup itself. The tag a talkgroup is filed
// under is the one its group tag names, and the label it is given is the one its own tag names, which is
// why the two fields hold different words here.
const third = {
  short_name: 'Harbour Bridge',
  talkgroup: 3001,
  talkgroup_description: 'Bridge Toll',
  talkgroup_group: 'Marine',
  talkgroup_group_tag: 'Toll',
  talkgroup_tag: 'Harbour Toll',
  timestamp: 1791288120000,
};

// The dsd+ recording names its system by reference rather than by label, and names a radio the server has
// never heard of. Its clock is a local time, which is why the capture runs the server with TZ=UTC.
const dsdRecordingName = '120000_1514000_P25_0001.1234-5_1514000[Dispatch]_6789[Car 4].mp3';

const out = args['--out'];

fs.mkdirSync(path.join(out, 'trunk'), { recursive: true });
fs.mkdirSync(path.join(out, 'dsd'), { recursive: true });

fs.writeFileSync(path.join(out, 'trunk', 'first.wav'), recording(2092));
fs.writeFileSync(path.join(out, 'trunk', 'first.json'), sidecar(first));

fs.writeFileSync(path.join(out, 'trunk', 'second.wav'), recording(2092));
fs.writeFileSync(path.join(out, 'trunk', 'second.json'), sidecar(second));

fs.writeFileSync(path.join(out, 'trunk', 'third.wav'), recording(2092));
fs.writeFileSync(path.join(out, 'trunk', 'third.json'), sidecar(third));

fs.writeFileSync(path.join(out, 'dsd', dsdRecordingName), recording(2048));
