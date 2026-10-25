// the display's rows: every row is a plain row placing its children
// at the ends with space-between, the name row is the big row, and
// the flag row carries each flag in its own cell, a direct child.
// the system row keeps its type cell and the flag row keeps DELAYED.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { afterEach, beforeEach, describe, it } from 'node:test';

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
