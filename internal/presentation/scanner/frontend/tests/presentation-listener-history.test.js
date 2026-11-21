// the history table: five fixed rows, blank before any entry is made,
// an entry recorded when the call after it arrives, the row being
// played set bold, and the four columns reading the call's labels
// with the raw values as fallbacks, the talkgroup column naming a
// talkgroup the way its type numbers it. the ring is the domain's.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { afterEach, beforeEach, describe, it } from 'node:test';

import { getAppContext } from '../application/context.js';

import { formatTime } from '../../../common/frontend/presentation/display-format.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/listener/rdio-display.js';

import { connectFields, findAll } from './dom-shim.js';

// the display records the call it leaves behind into the history,
// and that call lives inside the use-cases: the cases name it through
// the same reading the component takes, and hand it back after.
const previousSlot = () => {
  const { useCases } = getAppContext();

  const original = Object.getOwnPropertyDescriptor(useCases, 'callPrevious');
  const slot = { call: null };

  Object.defineProperty(useCases, 'callPrevious', {
    configurable: true,
    get: () => slot.call,
  });

  return { slot, restore: () => Object.defineProperty(useCases, 'callPrevious', original) };
};

let el;
let previous;

beforeEach(() => {
  previous = previousSlot();

  el = document.createElement('rdio-display');

  el.connectedCallback();
  connectFields(el);

  getAppContext().store.setState({
    auth: { status: 'anonymous', denied: false },
    call: undefined,
  });
});

afterEach(() => {
  el.disconnectedCallback();
  previous.restore();
});

const bodyRows = () => findAll(el, (node) => node.tag === 'tr' && node.parent?.tag === 'tbody');

const cells = (row) => row.children.filter((node) => node.tag === 'td');

const call = (id, extra = {}) => ({
  id,
  system: 10 + id,
  talkgroup: 100 + id,
  frequency: 855e6 + id,
  dateTime: 1721692800000 + id * 1000,
  audio: { data: [1] },
  ...extra,
});

describe('presentation/listener/rdio-display — the v6.6.3 history', () => {
  it('shows five fixed rows, blank before any entry is made', () => {
    const rows = bodyRows();

    assert.equal(rows.length, 5, 'the history does not hold five fixed rows');

    for (const row of rows) {
      const tds = cells(row);

      assert.equal(tds.length, 4, 'a history row does not carry the four columns');

      for (const td of tds) {
        assert.equal(td.children.length, 0, 'a history row is not blank before entries are made');
      }
    }
  });

  it('records the entry when the call after it arrives', () => {
    const { store } = getAppContext();

    const first = call(1);
    const second = call(2);

    previous.slot.call = first;

    store.setState({ call: second });

    const rows = bodyRows();

    assert.deepEqual(cells(rows[0]).map((td) => td.textContent), [
      formatTime(new Date(first.dateTime)), '11', '101', '855000001',
    ], 'the first row does not carry the call the display left behind');

    for (const row of rows.slice(1)) {
      for (const td of cells(row)) {
        assert.equal(td.children.length, 0, 'the rows below the entry are not blank');
      }
    }
  });

  it('sets the row being played bold', () => {
    const { store } = getAppContext();

    const first = call(1);

    previous.slot.call = first;

    store.setState({ call: call(2) });

    store.setState({ call: first });

    const rows = bodyRows();

    assert.equal(rows[0].className, 'replaying', 'the row being played is not set bold');
  });

  it('reads the columns on the labels with the raw values as their fallbacks', () => {
    const { store } = getAppContext();

    const labelled = call(1, {
      systemData: { label: 'Dispatch Sys' },
      talkgroupData: { label: 'Fire Dispatch', name: 'Fire Dispatch Name' },
    });

    previous.slot.call = labelled;

    store.setState({ call: call(2) });

    assert.deepEqual(cells(bodyRows()[0]).map((td) => td.textContent), [
      formatTime(new Date(labelled.dateTime)), 'Dispatch Sys', 'Fire Dispatch', 'Fire Dispatch Name',
    ], 'the columns do not read the labels');

    const bare = call(3);

    previous.slot.call = bare;

    store.setState({ call: call(4) });

    assert.deepEqual(cells(bodyRows()[0]).map((td) => td.textContent), [
      formatTime(new Date(bare.dateTime)), '13', '103', '855000003',
    ], 'the columns do not fall back to the raw values');
  });

  it('keeps one entry per call when the same call arrives again', () => {
    const { store } = getAppContext();

    const first = call(1);

    previous.slot.call = first;

    store.setState({ call: call(2) });

    previous.slot.call = first;

    store.setState({ call: call(3) });

    const filled = bodyRows().filter((row) => cells(row).some((td) => td.children.length > 0));

    assert.equal(filled.length, 1, 'the same call was recorded into the history twice');
  });

  it('names a talkgroup numbered by agency, fleet and subfleet in that form', () => {
    const { store } = getAppContext();

    const afs = call(1, {
      talkgroup: 1457,
      talkgroupData: { label: '1457', type: 'edacs' },
    });

    previous.slot.call = afs;

    store.setState({ call: call(2) });

    const row = cells(bodyRows()[0]);

    assert.equal(findAll(el, (node) => node.tag === 'th')[2]?.textContent, 'Talkgroup',
      'the column the history names the talkgroup in is not headed by its own column header');
    assert.equal(row[2].textContent, '11-061', 'the history does not name the talkgroup the way its type numbers it');
    assert.equal(row[2].getAttribute('title'), '11-061',
      'the history cell clips the form it shows and carries the decimal id as the name it clips');
    assert.equal(row[2].children.length, 1, 'the history cell holds more than the id it names');
  });

  it('names a talkgroup the operator named with the label they wrote', () => {
    const { store } = getAppContext();

    const covered = call(3, { talkgroup: 1457, talkgroupData: { label: 'Dispatch 1', type: 'edacs' } });

    previous.slot.call = covered;

    store.setState({ call: call(4) });

    assert.equal(cells(bodyRows()[0])[2].textContent, 'Dispatch 1',
      'the history does not name the talkgroup the operator named it with');
  });

  it('keeps the decimal id in the history for a type that carries no such form', () => {
    const { store } = getAppContext();

    const uncovered = call(5, { talkgroup: 1457, talkgroupData: { label: '1457', type: 'smartnet' } });

    previous.slot.call = uncovered;

    store.setState({ call: call(6) });

    assert.equal(cells(bodyRows()[0])[2].textContent, '1457',
      'the history writes an agency-fleet-subfleet form for a type that carries none');
  });
});
