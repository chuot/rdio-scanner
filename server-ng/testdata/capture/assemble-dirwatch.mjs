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

// assemble-dirwatch.mjs puts the frames the listener saw and the rows the server left behind into the one
// file the server-ng end-to-end test reads as its expectation.
//
//   node assemble-dirwatch.mjs --frames frames.json --dump dumpdir --out golden.json

import fs from 'node:fs';
import path from 'node:path';

const args = {};

for (let i = 2; i < process.argv.length; i += 2) {
  args[process.argv[i]] = process.argv[i + 1];
}

for (const name of ['--frames', '--dump', '--out']) {
  if (!args[name]) {
    console.error(`assemble-dirwatch.mjs: ${name} is required`);

    process.exit(2);
  }
}

const tables = ['calls', 'logs', 'groups', 'tags', 'talkgroups', 'units', 'systems'];

const golden = {
  frames: JSON.parse(fs.readFileSync(args['--frames'], 'utf8')),
};

for (const table of tables) {
  golden[table] = JSON.parse(fs.readFileSync(path.join(args['--dump'], `${table}.json`), 'utf8'));
}

fs.writeFileSync(args['--out'], JSON.stringify(golden, null, 2) + '\n');
