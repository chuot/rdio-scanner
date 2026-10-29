// the small line above the system row says the system's type exactly
// when the system row would hide it: a talkgroup carrying a different
// type, or the empty type the wire sends for an untyped talkgroup,
// pushes the system type up to the small line, and in every other case
// the type is said once, on the system row.
// see the display-system-type-line maintainer note.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { afterEach, beforeEach, describe, it } from 'node:test';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/listener/rdio-display.js';

import { connectFields, findAll } from './dom-shim.js';

import { getAppContext } from '../application/context.js';

const callWith = (systemType, talkgroupType) => ({
  id: 7,
  system: 1,
  talkgroup: 10,
  systemData: { label: 'Metro', ...(systemType ? { type: systemType } : {}) },
  ...(talkgroupType
    ? { talkgroupData: { label: 'Fire dispatch', name: 'Fire dispatch', type: talkgroupType } }
    : { talkgroupData: { label: 'Fire dispatch', name: 'Fire dispatch' } }),
  frequency: 155000,
  dateTime: Date.now(),
});

// the display runs a clock and a progress tick, so it is mounted in
// beforeEach and closed in afterEach, including when a case fails.
// the context's store is shared by every case, so its keys are
// cleared on both sides of it.
let el;

const store = () => getAppContext().store;

beforeEach(() => {
  store().setState({ call: null, map: undefined });

  el = document.createElement('rdio-display');

  el.connectedCallback();
  connectFields(el);
});

afterEach(() => {
  el.disconnectedCallback();
  store().setState({ call: null, map: undefined });
});

const span = (name) =>
  findAll(el, (node) => node.tag === 'span' && node.className.split(/\s+/).includes(name))[0];

// the small line and the system row's type cell, read after one call.
function typesFor(call) {
  store().setState({ call });

  const line = span('call-system-type');

  assert.ok(line, 'the display has no system type line above the system row');

  return { line: line.textContent, label: line.getAttribute('aria-label'), cell: span('call-type').textContent };
}

describe('presentation/listener/rdio-display — the system type line', () => {
  it('shows the system type when the talkgroup\'s own type hides it', () => {
    const { line, cell } = typesFor(callWith('P25', 'SmartNet'));

    assert.equal(line, 'P25',
      'the small line does not show the system type the system row hides');
    assert.equal(cell, 'SMARTNET',
      'the system row no longer shows the talkgroup\'s own type');
  });

  it('shows the system type when the talkgroup type arrives empty on the wire', () => {
    // the server names a type for every talkgroup, so an untyped one
    // reaches the display as the empty string rather than as a missing
    // entry: it hides the system type in the cell with nothing.
    const call = { ...callWith('P25', null), talkgroupData: { label: 'Ground', name: 'Ground', type: '' } };

    const { line, cell } = typesFor(call);

    assert.equal(line, 'P25',
      'the small line does not take the system type the empty talkgroup type hides');
    assert.equal(cell, '',
      'the system row\'s type cell changed what the wire gave it');
  });

  it('leaves the line empty while the system row shows the system type through its fallback', () => {
    const { line, cell } = typesFor(callWith('P25', null));

    assert.equal(line, '',
      'the small line repeats a system type the system row already shows');
    assert.equal(cell, 'P25',
      'the system row lost its fallback to the system type');
  });

  it('leaves the line empty when both types name the same standard in different case', () => {
    const { line } = typesFor(callWith('P25', 'p25'));

    assert.equal(line, '',
      'the small line repeats a type the system row already says under another case');
  });

  it('leaves the line empty when the system has no type to show', () => {
    const { line, cell } = typesFor(callWith(null, 'NXDN'));

    assert.equal(line, '',
      'the small line shows a type for a system that carries none');
    assert.equal(cell, 'NXDN',
      'the system row lost the talkgroup\'s own type');
  });

  it('fills the line only with a call and keeps it on the last call', () => {
    assert.equal(span('call-system-type')?.textContent ?? '', '',
      'the pristine display shows a system type line');

    const { line } = typesFor(callWith('P25', 'SmartNet'));

    assert.equal(line, 'P25', 'the line did not fill with the call');

    store().setState({ call: null });

    assert.equal(span('call-system-type').textContent, 'P25',
      'the system type line did not stay on screen with the other last-call rows');
  });

  it('names the line to assistive technology while it carries a type', () => {
    const { line, label } = typesFor(callWith('P25', 'SmartNet'));

    assert.equal(label, line,
      'the filled system type line does not carry its text as its accessible name');

    store().setState({ call: callWith('P25', 'P25') });

    const emptied = span('call-system-type');

    assert.equal(emptied.textContent, '');
    assert.equal(emptied.getAttribute('aria-label'), null,
      'the emptied system type line keeps an accessible name it no longer shows');
  });
});
