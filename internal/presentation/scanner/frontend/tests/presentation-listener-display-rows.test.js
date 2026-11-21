// the display's rows: every row is a plain row placing its children
// at the ends with space-between, the name row is the big row, and
// the flag row carries each flag in its own cell, a direct child.
// the system row keeps its type cell and the flag row keeps DELAYED,
// and the talkgroup row names the talkgroup the way its type calls for.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { afterEach, beforeEach, describe, it } from 'node:test';

import { getAppContext } from '../application/context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/listener/rdio-display.js';

import { connectFields, findAll } from './dom-shim.js';

// the display runs a clock and a progress tick; the test hands it the
// store it reads, mounts it in beforeEach and closes it in afterEach,
// so no timer outlives the case, including a case that fails.
let el;

beforeEach(() => {
  el = document.createElement('rdio-display');

  el.connectedCallback();
  connectFields(el);
});

afterEach(() => el.disconnectedCallback());

const classes = (node) => node.className.split(/\s+/);

describe('presentation/listener/rdio-display — the v6.6.3 rows', () => {
  it('asks no row for the three-column grid', () => {
    const gridded = findAll(el, (node) => node.tag === 'div' && classes(node).includes('three'));

    assert.deepEqual(gridded.map((node) => node.className), [],
      'a row asks for the three-column grid; the v6.6.3 row places its children with space-between');
  });

  it('asks no row for the full-row class', () => {
    const filled = findAll(el, (node) => node.tag === 'div' && classes(node).includes('full'));

    assert.deepEqual(filled.map((node) => node.className), [],
      'a row asks for the full-row class; the v6.6.3 name row is the big row');
  });

  it('carries each flag in its own cell of the flag row', () => {
    const flagRow = findAll(el, (node) =>
      node.tag === 'div' && classes(node).join(' ') === 'row right small')[0];

    assert.ok(flagRow, 'the display has no flag row to carry the flags');

    for (const name of ['temp-avoid', 'flag-avoid', 'flag-patch', 'flag-delayed']) {
      const flag = findAll(el, (node) => node.tag === 'span' && classes(node).includes(name))[0];

      assert.ok(flag, `the flag row has no ${name} flag`);
      assert.equal(flag.parent?.parent, flagRow,
        `the ${name} flag does not stand in its own cell of the flag row`);
    }
  });

  it('keeps the system row the system, type, and tag row it is', () => {
    const system = findAll(el, (node) => node.tag === 'span' && classes(node).includes('call-system'))[0];
    const type = findAll(el, (node) => node.tag === 'span' && classes(node).includes('call-type'))[0];
    const tag = findAll(el, (node) => node.tag === 'span' && classes(node).includes('call-tag'))[0];

    assert.ok(system && type && tag, 'the system row lost one of its cells');

    const row = system.parent.parent;

    assert.equal(row, type.parent.parent, 'the type cell stands outside the system row');
    assert.equal(row, tag.parent.parent, 'the tag cell stands outside the system row');
    assert.ok(classes(row).includes('row'), 'the system row is not a row');
  });

  it('keeps the history its four columns', () => {
    const heads = findAll(el, (node) => node.tag === 'th');

    assert.deepEqual(heads.map((node) => node.textContent), ['Time', 'System', 'Talkgroup', 'Name'],
      'the history table does not head its four columns');
  });
});

// the talkgroup row names the talkgroup the way its type calls for: the
// agency-fleet-subfleet form where the type carries one, the decimal id
// where it does not, and the label an operator wrote wherever there is one.

const shownCall = (extra) => ({
  id: 1,
  system: 11,
  talkgroup: 1457,
  frequency: 855123456,
  dateTime: 1721692800000,
  ...extra,
});

const show = (extra) => {
  const { store } = getAppContext();
  const auth = { status: 'anonymous', denied: false };

  store.setState({ auth, call: undefined });
  store.setState({ auth, call: shownCall(extra) });
};

const rowCell = (name) => findAll(el, (node) => node.tag === 'span' && classes(node).includes(name))[0];

describe('presentation/listener/rdio-display — the talkgroup the panel names', () => {
  it('writes the agency-fleet-subfleet form into the talkgroup cell', () => {
    show({ talkgroupData: { label: '1457', type: 'edacs' } });

    assert.equal(rowCell('call-talkgroup')?.textContent, '11-061',
      'the talkgroup cell does not write the form an edacs talkgroup is numbered in');
    assert.equal(rowCell('call-tgid')?.textContent, 'TGID: 11-061',
      'the id line does not write the form an edacs talkgroup is numbered in');
  });

  it('writes it when the system carries the type and the talkgroup does not', () => {
    show({ talkgroupData: { label: '1457' }, systemData: { type: 'edacs' } });

    assert.equal(rowCell('call-talkgroup')?.textContent, '11-061',
      'the talkgroup cell does not read the type the system carries');
    assert.equal(rowCell('call-tgid')?.textContent, 'TGID: 11-061',
      'the id line does not read the type the system carries');
  });

  it('keeps the decimal id for a talkgroup whose type carries no such form', () => {
    show({ talkgroupData: { label: '1457', type: 'smartnet' } });

    assert.equal(rowCell('call-talkgroup')?.textContent, '1457',
      'the talkgroup cell writes an agency-fleet-subfleet form for a type that has none');
    assert.equal(rowCell('call-tgid')?.textContent, 'TGID: 1457',
      'the id line writes an agency-fleet-subfleet form for a type that has none');
  });

  it('names the talkgroup with the label an operator wrote', () => {
    show({ talkgroupData: { label: 'Dispatch 1', type: 'edacs' } });

    assert.equal(rowCell('call-talkgroup')?.textContent, 'Dispatch 1',
      'the talkgroup cell does not name the talkgroup the operator named it with');
    assert.equal(rowCell('call-tgid')?.textContent, 'TGID: 11-061',
      'the id line does not keep the form the type calls for beside the operator\u2019s label');
  });

  it('carries the form as the cell\u2019s own text, and as nothing else', () => {
    show({ talkgroupData: { label: '1457', type: 'edacs' } });

    const cell = rowCell('call-talkgroup');

    assert.ok(cell, 'the display has no talkgroup cell');
    assert.equal(cell.children.length, 0, 'the talkgroup cell holds a second element beside the id');
    assert.equal(cell.getAttribute('aria-label'), '11-061', 'the talkgroup cell is not named by the text it shows');

    const row = cell.parent.parent;

    assert.deepEqual(findAll(el, (node) => node.tag !== '#text' && (node.textContent ?? '').includes('1457')), [],
      'the display carries the decimal id as a tooltip or a second element beside the form');

    const covered = classes(cell).join(' ');

    show({ talkgroupData: { label: '1457', type: 'smartnet' } });

    assert.equal(classes(rowCell('call-talkgroup')).join(' '), covered,
      'the form a talkgroup is written in is carried by a class the colour of the row reads');
  });
});
