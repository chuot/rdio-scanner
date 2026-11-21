// The shared display helpers the call views read: the locale hour cycle,
// the search table's duration, the bounds a date field is given, and the
// rule that decides the text a surface names a talkgroup by.

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';

import { formatDuration, formatTime, toLocalInputValue } from '../../../common/frontend/presentation/display-format.js';
import * as displayFormat from '../../../common/frontend/presentation/display-format.js';

test('a 12-hour locale renders the clock with the AM/PM suffix', () => {
  const date = new Date(2026, 9, 10, 13, 5);

  assert.equal(formatTime(date, 'en-US'), '01:05 PM');
  assert.equal(formatTime(new Date(2026, 9, 10, 0, 7), 'en-US'), '12:07 AM');
});

test('a 24-hour locale renders the clock in 24-hour time', () => {
  const date = new Date(2026, 9, 10, 13, 5);

  assert.equal(formatTime(date, 'de-DE'), '13:05');
  assert.equal(formatTime(new Date(2026, 9, 10, 0, 7), 'de-DE'), '00:07');
});

test('a non-date renders an empty clock', () => {
  assert.equal(formatTime(null, 'en-US'), '');
  assert.equal(formatTime('not a date', 'en-US'), '');
});

// The search table's duration column: mm:ss from the seconds the search row
// names, blank when the length was never measured.

test('a measured length renders minutes and seconds', () => {
  assert.equal(formatDuration(12.345), '00:12');
  assert.equal(formatDuration(75), '01:15');
  assert.equal(formatDuration(3600), '60:00');
  assert.equal(formatDuration(9.5), '00:09');
});

test('a length never measured renders blank', () => {
  assert.equal(formatDuration(0), '');
  assert.equal(formatDuration(-4), '');
  assert.equal(formatDuration(undefined), '');
  assert.equal(formatDuration(null), '');
  assert.equal(formatDuration('12'), '');
});

// the bounds a date field is given from the two ends of a search
// answer. a datetime-local field reads its bound only as a wall-clock
// minute: any other shape is a bound the field ignores.

test('an instant becomes the wall-clock minute the date field reads', () => {
  const local = new Date(2026, 9, 6, 23, 41, 7);

  assert.equal(toLocalInputValue(local.toISOString()), '2026-10-06T23:41');
});

test('the seconds an instant carries are not part of the bound', () => {
  const local = new Date(2026, 9, 6, 8, 0, 59, 999);

  assert.equal(toLocalInputValue(local.toISOString()), '2026-10-06T08:00');
});

test('a bound is not invented for an instant that is not there', () => {
  assert.equal(toLocalInputValue(''), '');
  assert.equal(toLocalInputValue(null), '');
  assert.equal(toLocalInputValue(undefined), '');
  assert.equal(toLocalInputValue('not an instant'), '');
});

test('a bound is not invented for the zero time an empty search answers with', () => {
  assert.equal(toLocalInputValue('0001-01-01T00:00:00Z'), '');
});

// The types whose talkgroup ids are eleven-bit group ids written as agency,
// fleet and subfleet: EDACS, and the Provoice trunking carried over the same
// scheme. Every other type's ids are looked up in a fleet map as decimal.

test('the agency-fleet-subfleet form follows an edacs talkgroup', () => {
  assert.equal(displayFormat.isAfsSystem({ talkgroupData: { type: 'edacs' } }), true);
});

test('the agency-fleet-subfleet form follows an edacs system', () => {
  assert.equal(displayFormat.isAfsSystem({ systemData: { type: 'edacs' } }), true);
});

test('the agency-fleet-subfleet form follows a provoice talkgroup or system', () => {
  assert.equal(displayFormat.isAfsSystem({ talkgroupData: { type: 'provoice' } }), true);
  assert.equal(displayFormat.isAfsSystem({ systemData: { type: 'provoice' } }), true);
});

test('every other system type keeps the decimal talkgroup id', () => {
  for (const type of ['smartnet', 'p25', 'dmr', 'nxdn', 'tetra', 'am', 'fm', 'nfm']) {
    assert.equal(displayFormat.isAfsSystem({ talkgroupData: { type }, systemData: { type } }), false, type);
  }

  assert.equal(displayFormat.isAfsSystem({ talkgroupData: { type: '' }, systemData: { type: '' } }), false);
  assert.equal(displayFormat.isAfsSystem({ talkgroupData: {}, systemData: {} }), false);
  assert.equal(displayFormat.isAfsSystem({}), false);
});

// The form reads 11 bits: agency 00-15, fleet 00-15, subfleet 0-7. An id the
// bits cannot carry is not a talkgroup in this scheme, and folding it into one
// would name a talkgroup that does not exist on the system.
//
// The pairs below are the shared table: this is what the display writes for each
// reference, and what a recording named by that form must read back to. Zero names
// no talkgroup, so no call is ever filed under the lowest form; the watcher's own
// read records that refusal.

const AFS_PAIRS = [
  [0, '00-000'],
  [1, '00-001'],
  [8, '00-010'],
  [1457, '11-061'],
  [2047, '15-157'],
];

test('the form splits the id into agency, fleet and subfleet', () => {
  for (const [reference, form] of AFS_PAIRS)
    assert.equal(displayFormat.formatAfs(reference), form, `the form written for reference ${reference}`);
});

test('an id the eleven bits cannot carry keeps its decimal form', () => {
  assert.equal(displayFormat.formatAfs(2048), '2048');
  assert.equal(displayFormat.formatAfs(2049), '2049');
  assert.equal(displayFormat.formatAfs(4095), '4095');
  assert.equal(displayFormat.formatAfs(12345), '12345');
  assert.equal(displayFormat.formatAfs(53102), '53102');
});

// Every production JavaScript the webapps ship: a second copy of the split rule
// would hide among these. A build bundle is the compiled output of these same files,
// and a test file is where a copy is read rather than written, so neither counts.
function walk(dir) {
  const found = [];

  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (['tests', 'bundle', 'node_modules'].includes(entry.name)) continue;

    const full = path.join(dir, entry.name);

    if (entry.isDirectory()) found.push(...walk(full));
    else if (entry.name.endsWith('.js')) found.push(full);
  }

  return found;
}

// The eleven bits are split in two places only: this module writes the form the
// listener reads, and the directory watcher reads the same form back out of a
// recording's filename. A recording filed under the name the listener was shown is
// the call the listener was shown, so the two ends have to split the bits the same
// way, and a formatter written again inside a web component is a third place they
// can drift apart in.

test('the eleven-bit split lives in the shared formatter and the watcher, and nowhere else', () => {
  // the code repository, the folder the tests sit five levels below.
  const repoRoot = path.resolve(import.meta.dirname, '../../../../..');
  const formatter = path.join(repoRoot, 'internal/presentation/common/frontend/presentation/display-format.js');
  const watcher = path.join(repoRoot, 'internal/infrastructure/dirwatch/parsers/mask.go');

  // The display end shifts the id down into agency, fleet and subfleet; the watcher
  // end shifts the three pieces back up into one reference.
  const displayEnd = (source) => />>\s*7\b/.test(source) && />>\s*3\b/.test(source) && /&\s*15\b/.test(source) && /&\s*7\b/.test(source);
  const watcherEnd = (source) => /<<\s*7\b/.test(source) && /<<\s*3\b/.test(source);

  assert.ok(displayEnd(fs.readFileSync(formatter, 'utf8')),
    'the shared formatter no longer carries the split the display writes');
  assert.ok(watcherEnd(fs.readFileSync(watcher, 'utf8')),
    'the directory watcher no longer carries the split the same pairs read back through');

  for (const file of walk(path.join(repoRoot, 'internal/presentation'))) {
    if (file === formatter) continue;

    assert.equal(displayEnd(fs.readFileSync(file, 'utf8')), false,
      `${path.relative(repoRoot, file)} splits the eleven bits again instead of calling the shared formatter`);
  }
});

// The text a surface names a talkgroup by: the label an operator wrote, else
// the form the type calls for, else the decimal id. Auto-populate labels a
// talkgroup it has never seen described with its own reference as text, and
// such a label is the id in disguise rather than a name.

test('a label an operator wrote names the talkgroup', () => {
  assert.equal(displayFormat.talkgroupText({ label: 'Dispatch 1', type: 'edacs' }, 1457, { type: 'edacs' }), 'Dispatch 1');
  assert.equal(displayFormat.talkgroupText({ label: 'Dispatch 1', type: 'dmr' }, 1457, { type: 'dmr' }), 'Dispatch 1');
  assert.equal(displayFormat.talkgroupText({ label: '1457', type: 'edacs' }, 991, { type: 'edacs' }), '1457');
});

test('the label auto-populate generated yields to the form the type calls for', () => {
  assert.equal(displayFormat.talkgroupText({ label: '1457', type: 'edacs' }, 1457, { type: 'edacs' }), '11-061');
  assert.equal(displayFormat.talkgroupText({ label: '', type: 'edacs' }, 1457, { type: 'edacs' }), '11-061');
  assert.equal(displayFormat.talkgroupText({ label: null, type: 'edacs' }, 1457, { type: 'edacs' }), '11-061');
  assert.equal(displayFormat.talkgroupText(undefined, 1457, { type: 'edacs' }), '11-061');
  assert.equal(displayFormat.talkgroupText({ label: '1457', type: '' }, 1457, { type: 'edacs' }), '11-061');
});

test('a talkgroup no type carries in this scheme keeps its decimal id', () => {
  assert.equal(displayFormat.talkgroupText({ label: '1457', type: 'smartnet' }, 1457, { type: 'smartnet' }), '1457');
  assert.equal(displayFormat.talkgroupText(undefined, 53102, { type: 'smartnet' }), '53102');
  assert.equal(displayFormat.talkgroupText(undefined, 53102, { type: 'edacs' }), '53102');
});
