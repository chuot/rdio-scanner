// The Options section answers the todo that sent the operator to one of its controls:
// the control the todo named is the row that reads as sent-to, the rows beside it are
// untouched, and the request is consumed so the mark does not outlive the visit.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-options.js';

import { findAll } from './dom-shim.js';

const OPTIONS = {
  audioConversion: 1,
  autoPopulate: false,
  branding: 'shed',
  disableDuplicateDetection: false,
  email: 'ops@example.com',
  keypadBeeps: 'uniden',
  maxClients: 10,
  pruneDays: 0,
  showListenersCount: false,
  sortTalkgroups: false,
};

// Each pass opens the section on its own fixture and closes it again: the section
// subscribes to the request, and a section left subscribed would consume the next
// pass's request before the section under test sees it.
function openOptions(request) {
  const context = getAdminContext();

  context.store.setState({
    focus: null,
    'section:options': {
      baseline: OPTIONS,
      working: { ...OPTIONS },
      dirty: false,
      invalid: false,
      conflict: false,
    },
  });

  const el = new (customElements.get('rdio-admin-options'))();

  el.connectedCallback();

  if (request) context.useCases.focusControl(...request);

  return { ...context, el, close: () => el.disconnectedCallback() };
}

function marked(rows) {
  return rows.filter((row) => row.className.includes('highlight')).map((row) => row.getAttribute("data-control"));
}

test('the control the todo named is the row that reads as sent-to', () => {
  const { el, close } = openOptions(['options', 'autoPopulate']);

  try {
    const rows = findAll(el, (node) => node.getAttribute("data-control"));

    assert.deepEqual(marked(rows), ['autoPopulate'],
      'the Auto Populate row must carry the mark and no other row may carry it');
  } finally {
    close();
  }
});

test('the request is consumed with the mark, so the mark does not outlive the visit', () => {
  const { store, close } = openOptions(['options', 'autoPopulate']);

  try {
    assert.equal(store.getState().focus, null,
      'the section must consume the request it answered');
  } finally {
    close();
  }
});

test('a control this section does not have leaves the request standing', () => {
  const { store, el, close } = openOptions(['options', 'noSuchControl']);

  try {
    assert.deepEqual(marked(findAll(el, (node) => node.getAttribute("data-control"))), [],
      'a control the section does not have marks nothing');

    assert.deepEqual(store.getState().focus, { section: 'options', control: 'noSuchControl' },
      'a request the section cannot answer is left for the section that can');
  } finally {
    close();
  }
});

test('a request naming another section leaves this section untouched', () => {
  const { store, el, close } = openOptions(['systems', 'autoPopulate']);

  try {
    assert.deepEqual(marked(findAll(el, (node) => node.getAttribute("data-control"))), [],
      'the Options section marks nothing for a request that is not its own');

    assert.deepEqual(store.getState().focus, { section: 'systems', control: 'autoPopulate' },
      'a request for another section is not consumed here');
  } finally {
    close();
  }
});

test('every control row names the control it edits, so a request can find it', () => {
  const { el, close } = openOptions();

  try {
    const controls = findAll(el, (node) => node.getAttribute("data-control")).map((row) => row.getAttribute("data-control"));

    assert.ok(controls.includes('autoPopulate'), 'the Auto Populate row must name its control');
    assert.ok(controls.includes('maxClients'), 'every control row must name its control');
  } finally {
    close();
  }
});
