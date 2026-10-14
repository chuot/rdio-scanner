// The systems-selection tree, and the keyboard that stays on the checkbox it just activated.
//
// The tree answers every checkbox by rebuilding itself: the cascade through the model changes the
// state of the rows above and below, so the page is painted from the model again. A rebuild throws
// away the control the keyboard was standing on, and a browser puts the focus on the page's body —
// so the operator who checks a talkgroup with the keyboard finds themselves at the top of the page,
// and the next Tab starts the walk down the tree again from the beginning. The tree has to put the
// keyboard back on the control it just answered, the same row it is looking at.
//
// The readings are taken on the control the operator activated, through the change event a click and
// the keyboard's Space both leave on a checkbox.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-systems-selection.js';

import { findAll } from './dom-shim.js';

// The config the sections hand the tree: the systems carry their `systemRef`, the talkgroups their
// `talkgroupRef`, and those are the identifiers the tree's rows are built from.
const CONFIG = {
  groups: [{ id: 1, label: 'Dispatch' }, { id: 2, label: 'Interop' }],
  tags: [{ id: 3, label: 'Fire' }],
  systems: [
    {
      systemRef: 10,
      label: 'Metro',
      talkgroups: [{ talkgroupRef: 100, label: 'Fire dispatch' }, { talkgroupRef: 101, label: 'Tac dispatch' }],
    },
    { systemRef: 11, label: 'County', talkgroups: [{ talkgroupRef: 110, label: 'Sheriff' }] },
  ],
};

function tree() {
  document.body.replaceChildren();

  const el = document.createElement('rdio-systems-selection');

  el.setup(CONFIG, null);

  document.body.append(el);

  return el;
}

// The tree's checkboxes, in the order the page presents them.
function boxes(el) {
  return findAll(el, (node) => node.tag === 'input' && node.type === 'checkbox');
}

// The checkbox naming a row of the tree: the row is what the operator is looking at, and the control
// the tree hands the keyboard back to has to be the same row, not the position the render happened
// to leave it in.
function boxFor(el, name) {
  return boxes(el).find((box) => box.dataset.node === name) ?? null;
}

// The operator activates a checkbox: the box flips, and the box answers with the change event a
// click and the keyboard's Space both leave on it. The tree rebuilds, and the keyboard is where the
// tree left it.
function activate(box) {
  box.checked = !box.checked;
  box.dispatchEvent({ type: 'change', target: box });
}

describe('presentation/admin/rdio-systems-selection — the keyboard stays on the row it activated', () => {
  it('names every row of the tree by the row itself', () => {
    const el = tree();

    const named = boxes(el).filter((box) => box.dataset.node);

    assert.equal(named.length, boxes(el).length, 'the tree has rows whose controls carry no row identity');
    assert.equal(new Set(named.map((box) => box.dataset.node)).size, named.length, 'two rows of the tree carry the same identity');
  });

  it('keeps the keyboard on a talkgroup checkbox the operator activated', () => {
    const el = tree();
    const talkgroup = boxFor(el, 'talkgroup:100');

    assert.ok(talkgroup, 'the tree does not name the talkgroup row');

    talkgroup.focus();

    activate(talkgroup);

    const after = boxFor(el, 'talkgroup:100');

    assert.equal(document.activeElement, after, `the keyboard ended on the page, not on the talkgroup row: ${document.activeElement?.tag}`);
    assert.equal(after.checked, true, 'the row the keyboard is standing on did not take the state the operator gave it');
  });

  it('keeps the keyboard on the Everything checkbox the operator activated', () => {
    const el = tree();
    const everything = boxFor(el, 'everything');

    assert.ok(everything, 'the tree does not name the Everything row');

    everything.focus();

    activate(everything);

    assert.equal(document.activeElement, boxFor(el, 'everything'), 'the keyboard left the row the operator activated');
  });

  it('keeps the keyboard on a system checkbox whose whole subtree changed', () => {
    const el = tree();
    const system = boxFor(el, 'system:10');

    assert.ok(system, 'the tree does not name the system row');

    system.focus();

    activate(system);

    const after = boxFor(el, 'system:10');

    assert.equal(document.activeElement, after, 'the keyboard left the system row its subtree just changed');

    // The cascade the model ran through the tree is the same one the pointer gets: the talkgroups
    // under the system follow the row the operator activated.
    const talkgroups = [boxFor(el, 'talkgroup:100'), boxFor(el, 'talkgroup:101')];

    assert.deepEqual(talkgroups.map((box) => box.checked), [true, true], 'the cascade did not follow the row the keyboard is standing on');
  });

  it('leaves the keyboard where the operator put it when the tree answers with no change of rows', () => {
    const el = tree();
    const group = boxFor(el, 'group:2');

    group.focus();

    activate(group);

    assert.equal(document.activeElement, boxFor(el, 'group:2'), 'the keyboard left the group row the operator activated');
  });

  it('keeps the keyboard on the tree when the operator activated a row from a control outside it', () => {
    const opener = document.createElement('button');

    opener.type = 'button';
    opener.textContent = 'Choose systems';
    document.body.append(opener);
    opener.focus();

    const el = tree();
    const tag = boxFor(el, 'tag:3');

    tag.focus();
    activate(tag);

    assert.equal(document.activeElement, boxFor(el, 'tag:3'), 'the keyboard was left outside the row it activated');
  });
});
