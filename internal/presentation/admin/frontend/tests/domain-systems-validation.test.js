// Systems section validation (domain). Mirrors the legacy admin.service.ts
// validators: systemRef/siteRef/talkgroupRef required + min + duplicate, unit
// ref/range rules, groupIds/tagId cross-references, blacklists format.

import test from 'node:test';
import assert from 'node:assert/strict';

import {
  siteErrors,
  systemErrors,
  systemsSectionInvalid,
  talkgroupErrors,
  unitErrors,
} from '../domain/systems-validation.js';

test('system: label required, systemRef required/min/duplicate', () => {
  const systems = [
    { id: 1, systemRef: 10, label: 'A' },
    { id: 2, systemRef: 10, label: '' },
  ];

  assert.deepEqual(systemErrors(systems[0], systems), {
    systemRef: 'Id is already defined',
  });

  assert.deepEqual(systemErrors(systems[1], systems), {
    systemRef: 'Id is already defined',
    label: 'Label is required',
  });

  assert.deepEqual(systemErrors({ systemRef: null, label: 'B' }, [{ systemRef: null, label: 'B' }]), {
    systemRef: 'Id is required',
  });

  assert.deepEqual(systemErrors({ systemRef: 0, label: 'B' }, [{ systemRef: 0, label: 'B' }]), {
    systemRef: 'Id is invalid',
  });
});

test('system: blacklists must be a comma separated list of talkgroup ids', () => {
  const base = { systemRef: 10, label: 'A' };

  assert.deepEqual(systemErrors({ ...base, blacklists: '12,34,56' }, [base]), {});
  assert.deepEqual(systemErrors({ ...base, blacklists: '12, 34' }, [base]), {
    blacklists: 'Comma separated list of talkgroup Ids',
  });
  assert.deepEqual(systemErrors({ ...base, blacklists: 'abc' }, [base]), {
    blacklists: 'Comma separated list of talkgroup Ids',
  });
  assert.deepEqual(systemErrors({ ...base, blacklists: '' }, [base]), {});
  assert.deepEqual(systemErrors(base, [base]), {});
});

test('site: siteRef required/min/duplicate within the system, label required', () => {
  const sites = [
    { id: 1, siteRef: 5, label: 'S1' },
    { id: 2, siteRef: 5, label: '' },
  ];

  assert.deepEqual(siteErrors(sites[0], sites), {
    siteRef: 'Id is already defined',
  });
  assert.deepEqual(siteErrors(sites[1], sites), {
    siteRef: 'Id is already defined',
    label: 'Label is required',
  });

  assert.deepEqual(siteErrors({ siteRef: 0, label: 'S' }, [{ siteRef: 0, label: 'S' }]), {
    siteRef: 'Id is invalid',
  });

  assert.deepEqual(siteErrors({ siteRef: null, label: 'S' }, [{ siteRef: null, label: 'S' }]), {
    siteRef: 'Id is required',
  });
});

test('talkgroup: required fields and talkgroupRef duplicate within the system', () => {
  const talkgroups = [
    { id: 1, talkgroupRef: 101, label: 'T1', name: 'N1', groupIds: [1], tagId: 2 },
    { id: 2, talkgroupRef: 101, label: '', name: '', groupIds: [], tagId: null },
  ];

  // duplicate refs mark every row carrying them, like the legacy validator
  assert.deepEqual(talkgroupErrors(talkgroups[0], talkgroups, { groupIds: [1], tagIds: [2] }), {
    talkgroupRef: 'Id is already defined',
  });

  assert.deepEqual(talkgroupErrors(talkgroups[0], [talkgroups[0]], { groupIds: [1], tagIds: [2] }), {});

  assert.deepEqual(talkgroupErrors(talkgroups[1], talkgroups, { groupIds: [1], tagIds: [2] }), {
    talkgroupRef: 'Id is already defined',
    label: 'Label is required',
    name: 'Name is required',
    groupIds: 'Group is required',
    tagId: 'Tag is required',
  });

  assert.deepEqual(
    talkgroupErrors({ talkgroupRef: 0, label: 'T', name: 'N', groupIds: [1], tagId: 2 }, [], { groupIds: [1], tagIds: [2] }),
    { talkgroupRef: 'Id is invalid' },
  );
});

test('talkgroup: groupIds and tagId must reference existing groups/tags', () => {
  const tg = { talkgroupRef: 101, label: 'T', name: 'N', groupIds: [99], tagId: 77 };

  assert.deepEqual(talkgroupErrors(tg, [tg], { groupIds: [1, 2], tagIds: [3] }), {
    groupIds: 'Group is required',
    tagId: 'Tag is required',
  });

  // legacy quirk: a single number (not an array) is membership-checked too
  assert.deepEqual(talkgroupErrors({ ...tg, tagId: 3, groupIds: 99 }, [tg], { groupIds: [1], tagIds: [3] }), {
    groupIds: 'Group is required',
  });
  assert.deepEqual(
    talkgroupErrors({ ...tg, groupIds: 1, tagId: 3 }, [tg], { groupIds: [1], tagIds: [3] }),
    {},
  );
});

test('talkgroup: frequency cannot be negative', () => {
  const tg = { talkgroupRef: 101, label: 'T', name: 'N', groupIds: [1], tagId: 2, frequency: -1 };

  assert.deepEqual(talkgroupErrors(tg, [tg], { groupIds: [1], tagIds: [2] }), {
    frequency: 'Invalid frequency',
  });

  assert.deepEqual(
    talkgroupErrors({ ...tg, frequency: 0 }, [tg], { groupIds: [1], tagIds: [2] }),
    {},
  );
});

test('unit: unitRef required when the range is incomplete, duplicate otherwise', () => {
  const units = [
    { id: 1, unitRef: 7, unitFrom: 1, unitTo: 2, label: 'U1' },
    { id: 2, unitRef: 7, unitFrom: 3, unitTo: 4, label: 'U2' },
  ];

  assert.deepEqual(unitErrors(units[1], units), { unitRef: 'Id value is already defined' });

  // no unitRef and no range → required
  assert.deepEqual(unitErrors({ unitRef: null, unitFrom: null, unitTo: null, label: 'U' }, []), {
    unitRef: 'Id value is required',
  });

  // no unitRef but a full range → acceptable
  assert.deepEqual(unitErrors({ unitRef: null, unitFrom: 1, unitTo: 2, label: 'U' }, []), {});

  assert.deepEqual(unitErrors({ unitRef: 0, unitFrom: 1, unitTo: 2, label: 'U' }, []), {
    unitRef: 'Id value is invalid',
  });
});

test('unit: range from/to min and ordering', () => {
  assert.deepEqual(unitErrors({ unitFrom: 5, unitTo: 5, label: 'U' }, []), {
    unitFrom: 'From value should be lower than To value',
    unitTo: 'To value should be higher than From value',
  });

  assert.deepEqual(unitErrors({ unitFrom: 0, unitTo: 1, label: 'U' }, []), {
    unitFrom: 'From value is invalid',
  });

  assert.deepEqual(unitErrors({ unitFrom: 1, unitTo: 0, label: 'U' }, []), {
    unitFrom: 'From value should be lower than To value',
    unitTo: 'To value should be higher than From value',
  });

  assert.deepEqual(unitErrors({ unitFrom: 1, unitTo: 2, label: '' }, []), {
    label: 'Label is required',
  });
});

test('systemsSectionInvalid aggregates every level of the tree', () => {
  const groups = [{ id: 1 }, { id: 2 }];
  const tags = [{ id: 3 }];

  const valid = [
    {
      id: 1,
      systemRef: 10,
      label: 'A',
      sites: [{ id: 1, siteRef: 1, label: 'S' }],
      talkgroups: [{ id: 1, talkgroupRef: 101, label: 'T', name: 'N', groupIds: [1], tagId: 3 }],
      units: [{ id: 1, unitRef: 1, unitFrom: 1, unitTo: 2, label: 'U' }],
    },
  ];

  assert.equal(systemsSectionInvalid(valid, groups, tags), false);

  assert.equal(
    systemsSectionInvalid([{ ...valid[0], label: '' }], groups, tags),
    true,
  );
  assert.equal(
    systemsSectionInvalid([{ ...valid[0], sites: [{ siteRef: 1, label: '' }] }], groups, tags),
    true,
  );
  assert.equal(
    systemsSectionInvalid(
      [{ ...valid[0], talkgroups: [{ talkgroupRef: 101, label: 'T', name: 'N', groupIds: [], tagId: 3 }] }],
      groups,
      tags,
    ),
    true,
  );
  assert.equal(
    systemsSectionInvalid([{ ...valid[0], units: [{ unitRef: 1, unitFrom: 2, unitTo: 1, label: 'U' }] }], groups, tags),
    true,
  );
});
