// tri-state selection: everything cascades to groups, tags, systems, talkgroups
// and back in all four directions; emitted ids are business refs

import test from 'node:test';
import assert from 'node:assert/strict';

import {
  createSelectionModel,
  emitSelection,
  hydrateSelection,
  setEverything,
  setGroup,
  setSystem,
  setTag,
  setTalkgroup,
} from '../domain/systems-selection.js';

// Config fixture:
//   system 10 (TESTSYS): TG 101 (group 1 "Dispatch", tag 2 "Law"), TG 102 (group 2 "Fire", no tag)
//   system 20 (OTHER):   TG 201 (group 1, tag 3 "EMS")
function fixture() {
  return {
    groups: [{ id: 1, label: 'Dispatch' }, { id: 2, label: 'Fire' }],
    tags: [{ id: 2, label: 'Law' }, { id: 3, label: 'EMS' }],
    systems: [
      {
        systemRef: 10,
        label: 'TESTSYS',
        talkgroups: [
          { talkgroupRef: 101, label: 'TG1', groupIds: [1], tagId: 2 },
          { talkgroupRef: 102, label: 'TG2', groupIds: [2], tagId: 0 },
        ],
      },
      {
        systemRef: 20,
        label: 'OTHER',
        talkgroups: [
          { talkgroupRef: 201, label: 'TG3', groupIds: [1], tagId: 3 },
          { talkgroupRef: 202, label: 'TG4', groupIds: [2], tagId: 0 },
        ],
      },
    ],
  };
}

test('model is built from business refs, not database ids', () => {
  const model = createSelectionModel(fixture());

  assert.deepEqual(model.systems.map((s) => s.id), [10, 20]);
  assert.deepEqual(model.systems[0].talkgroups.map((t) => t.id), [101, 102]);
  assert.deepEqual(model.groups.map((g) => g.id), [1, 2]);
  assert.deepEqual(model.tags.map((t) => t.id), [2, 3]);
  assert.equal(model.all, false);
});

// the model keeps configuration labels, so the tree can name its rows
test('the model rows carry the labels the configuration carries', () => {
  const model = createSelectionModel(fixture());

  assert.deepEqual(model.groups.map((group) => group.label), ['Dispatch', 'Fire']);
  assert.deepEqual(model.tags.map((tag) => tag.label), ['Law', 'EMS']);
  assert.deepEqual(model.systems.map((system) => system.label), ['TESTSYS', 'OTHER']);
  assert.deepEqual(model.systems[0].talkgroups.map((talkgroup) => talkgroup.label), ['TG1', 'TG2']);
  assert.deepEqual(model.systems[1].talkgroups.map((talkgroup) => talkgroup.label), ['TG3', 'TG4']);
});

// a newly added row is carried with its empty label and no identifier
// placeholder words belong to the section that made the row
test('a row the add control created is carried with its empty label and no identifier', () => {
  const model = createSelectionModel({
    groups: [{ alert: '', label: '', led: '', order: 0 }],
    tags: [{ alert: '', label: '', led: '', order: 0 }],
    systems: [{ systemRef: null, label: '', talkgroups: [{ talkgroupRef: null, label: '', groupIds: [], tagId: 0 }] }],
  });

  assert.equal(model.groups[0].id, undefined, 'the model invented an identifier for a group that has none');
  assert.equal(model.groups[0].label, '');
  assert.equal(model.tags[0].id, undefined, 'the model invented an identifier for a tag that has none');
  assert.equal(model.tags[0].label, '');
  assert.equal(model.systems[0].id, null);
  assert.equal(model.systems[0].label, '');
  assert.equal(model.systems[0].talkgroups[0].id, null);
  assert.equal(model.systems[0].talkgroups[0].label, '');
});

test('checking Everything cascades to every group, tag, system and talkgroup', () => {
  const model = createSelectionModel(fixture());

  setEverything(model, true);

  assert.equal(model.all, true);
  assert.equal(model.indeterminate.everything, false);
  assert.deepEqual(model.groups.map((g) => g.checked), [true, true]);
  assert.deepEqual(model.tags.map((t) => t.checked), [true, true]);
  assert.deepEqual(model.systems.map((s) => s.all), [true, true]);
  assert.deepEqual(model.systems[0].talkgroups.map((t) => t.checked), [true, true]);
  assert.deepEqual(model.systems[1].talkgroups.map((t) => t.checked), [true, true]);
});

test('checking a group cascades to every talkgroup carrying that group, in every system', () => {
  const model = createSelectionModel(fixture());

  setGroup(model, 0, true); // Dispatch → TG101 and TG201

  assert.deepEqual([101, 201].map((ref) => findTg(model, ref).checked), [true, true]);
  assert.equal(findTg(model, 102).checked, false);

  setGroup(model, 0, false);

  assert.deepEqual([101, 201].map((ref) => findTg(model, ref).checked), [false, false]);
});

test('checking a tag cascades to every talkgroup carrying that tag', () => {
  const model = createSelectionModel(fixture());

  setTag(model, 1, true); // EMS → TG201

  assert.equal(findTg(model, 201).checked, true);
  assert.equal(findTg(model, 101).checked, false);
});

test('system all cascades down to its talkgroups', () => {
  const model = createSelectionModel(fixture());

  setSystem(model, 0, true);

  assert.deepEqual(model.systems[0].talkgroups.map((t) => t.checked), [true, true]);
  assert.equal(model.systems[0].all, true);
  assert.equal(model.systems[1].all, false);
});

test('talkgroup changes propagate up: partial → indeterminate, full → system all', () => {
  const model = createSelectionModel(fixture());

  setTalkgroup(model, 0, 0, true);

  assert.equal(model.indeterminate.systems[0], true);
  assert.equal(model.systems[0].all, false);

  setTalkgroup(model, 0, 1, true);

  assert.equal(model.indeterminate.systems[0], false);
  assert.equal(model.systems[0].all, true);

  setTalkgroup(model, 0, 0, false);

  assert.equal(model.indeterminate.systems[0], true);
  assert.equal(model.systems[0].all, false);
});

test('system states propagate up to Everything', () => {
  const model = createSelectionModel(fixture());

  setSystem(model, 0, true);

  assert.equal(model.indeterminate.everything, true);
  assert.equal(model.all, false);

  setSystem(model, 1, true);

  assert.equal(model.indeterminate.everything, false);
  assert.equal(model.all, true);
});

test('group state is derived from its talkgroups across all systems', () => {
  const model = createSelectionModel(fixture());

  // Dispatch covers TG101 (system 10) and TG201 (system 20).
  setTalkgroup(model, 0, 0, true);

  assert.equal(model.indeterminate.groups[0], true);
  assert.equal(model.groups[0].checked, false);

  setTalkgroup(model, 1, 0, true);

  assert.equal(model.indeterminate.groups[0], false);
  assert.equal(model.groups[0].checked, true);
});

test('tag state is derived from its talkgroups', () => {
  const model = createSelectionModel(fixture());

  setTalkgroup(model, 1, 0, true); // TG201 has tag EMS

  assert.equal(model.indeterminate.tags[1], false);
  assert.equal(model.tags[1].checked, true);
});

test('hydrate "*" checks everything', () => {
  const model = createSelectionModel(fixture());

  hydrateSelection(model, '*');

  assert.equal(model.all, true);
  assert.deepEqual(model.systems.map((s) => s.all), [true, true]);
});

test('hydrate a list of system ids checks those systems entirely', () => {
  const model = createSelectionModel(fixture());

  hydrateSelection(model, [10]);

  assert.equal(model.systems[0].all, true);
  assert.equal(model.systems[1].all, false);
  assert.equal(model.all, false);
  assert.equal(model.indeterminate.everything, true);
});

test('hydrate {id, talkgroups: "*"} checks the whole system', () => {
  const model = createSelectionModel(fixture());

  hydrateSelection(model, [{ id: 20, talkgroups: '*' }]);

  assert.equal(model.systems[1].all, true);
  assert.deepEqual(model.systems[1].talkgroups.map((t) => t.checked), [true, true]);
});

test('hydrate {id, talkgroups: [refs]} checks individual talkgroups', () => {
  const model = createSelectionModel(fixture());

  hydrateSelection(model, [{ id: 10, talkgroups: [101] }]);

  assert.equal(findTg(model, 101).checked, true);
  assert.equal(findTg(model, 102).checked, false);
  assert.equal(model.systems[0].all, false);
  assert.equal(model.indeterminate.systems[0], true);
});

test('emit "*" when Everything is checked', () => {
  const model = createSelectionModel(fixture());

  setEverything(model, true);

  assert.equal(emitSelection(model), '*');
});

test('emit omits untouched systems, uses "*" for whole systems and ref lists otherwise', () => {
  const model = createSelectionModel(fixture());

  setSystem(model, 0, true);
  setTalkgroup(model, 1, 0, true);

  assert.deepEqual(emitSelection(model), [
    { id: 10, talkgroups: '*' },
    { id: 20, talkgroups: [201] },
  ]);
});

test('emit an empty list when nothing is selected', () => {
  const model = createSelectionModel(fixture());

  assert.deepEqual(emitSelection(model), []);
});

function findTg(model, ref) {
  for (const system of model.systems) {
    const talkgroup = system.talkgroups.find((t) => t.id === ref);

    if (talkgroup) return talkgroup;
  }

  throw new Error(`talkgroup ${ref} not found`);
}
