// the guard against drift: a section's marks must match the fields its rule
// requires, the named exemptions aside; readings and exemptions are in the hub
// see the required-marker-declaration-drift maintainer note.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-systems.js';
import '../presentation/admin/rdio-admin-sites.js';
import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-units.js';
import '../presentation/admin/rdio-admin-groups.js';
import '../presentation/admin/rdio-admin-tags.js';
import '../presentation/admin/rdio-admin-access.js';
import '../presentation/admin/rdio-admin-apikeys.js';
import '../presentation/admin/rdio-admin-downstreams.js';
import '../presentation/admin/rdio-admin-dirwatch.js';
import '../presentation/admin/rdio-admin-options.js';
import '../presentation/admin/rdio-admin-tools.js';

import { connectFields, findAll, requiredMark } from './dom-shim.js';

import { groupRowErrors } from '../presentation/admin/rdio-admin-groups.js';
import { tagRowErrors } from '../presentation/admin/rdio-admin-tags.js';
import { accessRowErrors } from '../presentation/admin/rdio-admin-access.js';
import { apiKeyRowErrors } from '../presentation/admin/rdio-admin-apikeys.js';
import { downstreamRowErrors } from '../presentation/admin/rdio-admin-downstreams.js';
import { optionsFieldErrors } from '../presentation/admin/rdio-admin-options.js';
import { siteErrors, systemErrors, talkgroupErrors, unitErrors } from '../domain/systems-validation.js';
import { validateDirwatchRow } from '../domain/dirwatch.js';
import { validatePasswordChange } from '../domain/password.js';

const METRO = { id: 1, label: 'Metro', order: 2, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };
const COUNTY = { id: 2, label: 'County', order: 1, systemRef: 2, blacklists: '', sites: [], talkgroups: [], units: [] };

const SITES = { 1: [], 2: [{ id: 4, label: 'East', siteRef: 40, order: 1 }] };
const TALKGROUPS = { 1: [], 2: [{ id: 7, label: 'Fire', name: 'Fire dispatch', talkgroupRef: 70, order: 1, groupIds: [1], tagId: 3, alert: '', delay: null, frequency: null, led: '', type: '' }] };
const UNITS = { 1: [], 2: [{ id: 9, label: 'Car', unitRef: 100, unitFrom: 100, unitTo: 199, order: 1 }] };

function slice(working) {
  return { baseline: structuredClone(working), working: structuredClone(working), dirty: false, invalid: false, conflict: false };
}

function seed(store) {
  const state = { auth: { authenticated: true, docker: false, passwordNeedChange: false }, alerts: { fire: {} } };

  for (const [level, bySystem] of [['sites', SITES], ['talkgroups', TALKGROUPS], ['units', UNITS]]) {
    for (const system of [COUNTY, METRO]) {
      state[`subsection:${level}:${system.id}`] = slice(bySystem[system.id]);
    }
  }

  store.setState({
    ...state,
    'section:systems': slice([METRO, COUNTY]),
    'section:groups': slice([{ id: 1, label: 'Dispatch', alert: '', led: '' }]),
    'section:tags': slice([{ id: 3, label: 'Fire', alert: '', led: '' }]),
    'section:access': slice([{ id: 1, order: 1, code: 'alpha', ident: 'AAA', systems: '*' }]),
    'section:apikeys': slice([{ id: 1, order: 1, key: 'first-key', ident: 'K1', systems: '*', disabled: false }]),
    'section:downstreams': slice([{ id: 1, order: 1, url: 'https://first.example', apikey: 'dk-1', systems: '*', disabled: false }]),
    'section:dirwatch': slice([{ id: 1, order: 1, directory: '/var/spool/first', extension: 'mp3', mask: '#SYS #TG', type: 'default', systemId: null, siteId: null, talkgroupId: null, frequency: 1, delay: 2000, disabled: false }]),
    'section:options': slice({ audioConversion: 'opus', autoPopulate: false, branding: 'Ridio', disableDuplicateDetection: 0, email: 'ops@example.test', keypadBeeps: 'standard', maxClients: 5, pruneDays: 7, showListenersCount: 0, sortTalkgroups: false }),
  });

  return store;
}

function attach(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

function addRow(el) {
  findAll(el, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes('add'))[0].click();
  connectFields(el);

  return findAll(el, (node) => node.tag === 'details')[0]
    ?? findAll(el, (node) => node.tag === 'div' && node.className?.split(/\s+/).includes('rdio-admin-row'))[0];
}

function spanClass(node) {
  return node.className?.split(/\s+/).find((name) => name === 'field-title' || name === 'body');
}

function markedNames(root) {
  const names = new Set();

  for (const node of findAll(root, (child) => child.tag === 'rdio-form-field')) {
    const label = findAll(node, (child) => child.tag === 'label')[0];

    if (label && requiredMark(label)) names.add(label.textContent);
  }

  for (const span of findAll(root, (child) => spanClass(child))) {
    if (requiredMark(span)) names.add(span.textContent);
  }

  return [...names].sort();
}

function passwordPanel() {
  return findAll(attach('rdio-admin-tools'), (node) => node.className?.split(/\s+/).includes('rdio-admin-row'))
    .find((panel) => findAll(panel, (child) => child.className?.split(/\s+/).includes('row-title'))[0]?.textContent === 'Change password');
}

// requiredFields keeps only messages ending ' is required'; shape rejections mark nothing
function requiredFields(errors, names) {
  const keys = Object.entries(errors).filter(([, message]) => / is required$/.test(message)).map(([key]) => key);

  // a rule requiring a field the section's table does not name is drift to report, not swallow
  const unnamed = keys.filter((key) => !names[key]);

  return { fields: keys.map((key) => names[key]).filter(Boolean), unnamed };
}

// drift: undeclared names the rule requires but the page leaves unmarked, unbacked names the
// page marks with no requirement behind it; exemptions cover conditional requirements
function drift(requirements, declarations, exemptions = []) {
  const required = [...new Set(requirements)].filter((name) => !exemptions.includes(name)).sort();
  const declared = [...new Set(declarations)].sort();

  return {
    undeclared: required.filter((name) => !declared.includes(name)),
    unbacked: declared.filter((name) => !required.includes(name)),
  };
}

const SECTIONS = [
  {
    section: 'systems',
    page: () => addRow(attach('rdio-admin-systems')),
    rule: () => systemErrors({ label: '', systemRef: null, blacklists: '' }, []),
    names: { systemRef: 'Id', label: 'Label' },
  },
  {
    section: 'sites',
    page: () => addRow(attach('rdio-admin-sites')),
    rule: () => siteErrors({ label: '', siteRef: null }, []),
    names: { siteRef: 'Id', label: 'Label' },
  },
  {
    section: 'talkgroups',
    page: () => addRow(attach('rdio-admin-talkgroups')),
    rule: () => talkgroupErrors({ label: '', name: '', talkgroupRef: null, groupIds: [], tagId: null, frequency: null }, [], { groupIds: [1], tagIds: [3] }),
    names: { talkgroupRef: 'Id', label: 'Label', name: 'Name', groupIds: 'Groups', tagId: 'Tag' },
  },
  {
    section: 'units',
    page: () => addRow(attach('rdio-admin-units')),
    rule: () => unitErrors({ label: '', unitRef: undefined, unitFrom: undefined, unitTo: undefined }, []),
    names: { unitRef: 'Id', label: 'Label' },
    exemptions: ['Id'],
  },
  {
    section: 'groups',
    page: () => addRow(attach('rdio-admin-groups')),
    rule: () => groupRowErrors({ label: '', alert: '', led: '' }),
    names: { label: 'Label' },
  },
  {
    section: 'tags',
    page: () => addRow(attach('rdio-admin-tags')),
    rule: () => tagRowErrors({ label: '', alert: '', led: '' }),
    names: { label: 'Label' },
  },
  {
    section: 'access',
    page: () => addRow(attach('rdio-admin-access')),
    rule: () => {
      const row = { code: '', ident: '', systems: null };

      return accessRowErrors(row, 0, [row]);
    },
    names: { code: 'Code', ident: 'Ident', systems: 'Systems' },
    exemptions: ['Systems'],
  },
  {
    section: 'API keys',
    page: () => addRow(attach('rdio-admin-apikeys')),
    rule: () => {
      const row = { key: '', ident: '', systems: null };

      return apiKeyRowErrors(row, 0, [row]);
    },
    names: { key: 'Key', ident: 'Ident', systems: 'Systems' },
    exemptions: ['Systems'],
  },
  {
    section: 'downstreams',
    page: () => addRow(attach('rdio-admin-downstreams')),
    rule: () => {
      const row = { url: '', apikey: '', systems: null };

      return downstreamRowErrors(row, 0, [row]);
    },
    names: { url: 'URL', apikey: 'API key', systems: 'Systems' },
    exemptions: ['Systems'],
  },
  {
    section: 'dirwatch',
    page: () => addRow(attach('rdio-admin-dirwatch')),
    rule: () => {
      const row = { directory: '', extension: '', mask: '', type: 'default', systemId: null, siteId: null, talkgroupId: null };

      return validateDirwatchRow(row, [row]);
    },
    names: { directory: 'Directory', systemId: 'System', talkgroupId: 'Talkgroup' },
    exemptions: ['System', 'Talkgroup'],
  },
  {
    section: 'options',
    page: () => attach('rdio-admin-options'),
    rule: () => optionsFieldErrors({}),
    names: { maxClients: 'Max Clients', pruneDays: 'Prune Days', keypadBeeps: 'Keypad Beep Style' },
  },
  {
    section: 'change password',
    page: () => passwordPanel(),
    rule: () => validatePasswordChange({ currentPassword: '', newPassword: '', verifyNewPassword: '' }),
    names: { currentPassword: 'Current password', newPassword: 'New password', verifyNewPassword: 'Verify new password' },
  },
];

for (const { section, page, rule, names, exemptions = [] } of SECTIONS) {
  test(`the ${section} section marks exactly the fields its rule requires`, () => {
    seed(getAdminContext().store);

    const { fields, unnamed } = requiredFields(rule(), names);

    assert.deepEqual(unnamed, [], `${section}: the rule requires fields the section's table does not name`);
    assert.deepEqual(
      drift(fields, markedNames(page()), exemptions),
      { undeclared: [], unbacked: [] },
      `${section}: the marks on the row and the requirements of the rule are the same fields`,
    );
  });
}

// ---------------------------------------------------------------------------
// The guard bites: the two ways a section and its rule come apart.
// ---------------------------------------------------------------------------

test('a rule that requires a field no section declares is reported', () => {
  // the access row's code: a declaration naming only the identifier hides the requirement
  assert.deepEqual(drift(['Code', 'Ident'], ['Ident']), { undeclared: ['Code'], unbacked: [] }, 'the requirement the page does not mark is named');
});

test('a declaration with no rule behind it is reported', () => {
  // the groups mark on a row whose rule takes no groups claims a field it accepts empty
  assert.deepEqual(drift(['Ident'], ['Groups', 'Ident']), { undeclared: [], unbacked: ['Groups'] }, 'the mark with no requirement behind it is named');
});

test('a conditional requirement is exempt and the rest of the row is guarded', () => {
  // the unit row: the rule asks for the identifier only when it is left empty, so it is exempt
  assert.deepEqual(drift(['Id', 'Label'], ['Label'], ['Id']), { undeclared: [], unbacked: [] }, 'the conditional identifier is exempt and the label is guarded');

  // The exemption does not blind the guard to the rest of the row.
  assert.deepEqual(drift(['Id', 'Label', 'Range from'], ['Label'], ['Id']), { undeclared: ['Range from'], unbacked: [] }, 'the exemption covers its own field and nothing else');
});
