// systems-tree row rule: one row open at a time, plus the scroll back after rows
// above the opened row collapse; arithmetic over section state, no DOM or clock

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { rowsToClose, scrollCompensation } from '../domain/row-open-rule.js';

describe('domain/row-open-rule — the rows the section closes when one opens', () => {
  it('names every other open row to close when the operator opens a row', () => {
    assert.deepEqual(rowsToClose(['tg:id1', 'tg:id2'], 'tg:id2'), ['tg:id1']);
  });

  it('names nothing to close when the row the operator opened is the only open row', () => {
    assert.deepEqual(rowsToClose(['tg:id2'], 'tg:id2'), []);
  });

  it('names nothing to close when the row the operator closed was not open', () => {
    assert.deepEqual(rowsToClose(['tg:id1', 'tg:id2'], 'tg:id3'), []);
  });

  it('names the open rows it is given, in the order it is given them, and keeps exactly one row open', () => {
    const closed = rowsToClose(['tg:id1', 'tg:id2', 'tg:id3'], 'tg:id2');

    assert.deepEqual(closed, ['tg:id1', 'tg:id3']);
    assert.ok(!closed.includes('tg:id2'), 'the row the operator opened is never among the rows to close');
  });

  it('names nothing to close when no row is open', () => {
    assert.deepEqual(rowsToClose([], 'tg:id1'), []);
  });
});

describe('domain/row-open-rule — the scroll the section gives back', () => {
  it('is the distance the opened row moved', () => {
    assert.equal(scrollCompensation(300, 120), -180);
  });

  it('is the distance the opened row moved when the rows above it grew the content', () => {
    assert.equal(scrollCompensation(120, 300), 180);
  });

  it('is nothing when the row did not move', () => {
    assert.equal(scrollCompensation(240, 240), 0);
  });

  it('is nothing when a measurement is missing', () => {
    for (const missing of [Number.NaN, undefined, null, 'top', Infinity]) {
      assert.equal(scrollCompensation(missing, 120), 0, `no measurement above (${missing}) yields no compensation`);
      assert.equal(scrollCompensation(300, missing), 0, `no measurement below (${missing}) yields no compensation`);
    }
  });
});
