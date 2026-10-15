// The administration page says what changed while the operator was not looking at it.
//
// The operator works on a section, and the page answers: the save bar reports what the server did
// with the rows, and a row that moved is a row whose place in the list the listener plays in. The
// page carries one polite region, empty of anything static, and writes into it in the page's own
// words: the save's answer, and the row that moved with the place it landed on.
//
// The readings are taken on the rendered shell and on the region's own text, after the control the
// operator pressed.

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import './dom-shim.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin.js';
import '../presentation/admin/rdio-admin-systems.js';
import '../presentation/admin/rdio-admin-sites.js';
import '../presentation/admin/rdio-admin-talkgroups.js';
import '../presentation/admin/rdio-admin-units.js';

import { findAll } from './dom-shim.js';

import { getAdminContext } from '../application/admin-context.js';

const METRO = { id: 1, label: 'Metro', order: 1, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };
const COUNTY = { id: 2, label: 'County', order: 2, systemRef: 2, blacklists: '', sites: [], talkgroups: [], units: [] };
const DEPOT = { id: 3, label: 'Depot', order: 3, systemRef: 3, blacklists: '', sites: [], talkgroups: [], units: [] };

function slice(value) {
  return { baseline: structuredClone(value), working: structuredClone(value), dirty: true, invalid: false, conflict: false };
}

// The shell, on the systems section: the section the operator orders, and the section the save bar
// below it answers for.
function shell() {
  getAdminContext().store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    todos: [],
    alerts: {},
    'section:access': slice([]),
    'section:apikeys': slice([]),
    'section:dirwatch': slice([]),
    'section:downstreams': slice([]),
    'section:groups': slice([]),
    'section:options': slice({ branding: 'Harness Dispatch' }),
    'section:systems': slice([METRO, COUNTY, DEPOT]),
    'section:tags': slice([]),
  });

  const el = new (customElements.get('rdio-admin'))();

  el.connectedCallback();

  document.body.replaceChildren(el);

  el.openSection('systems');

  // The shell places the section card and its save bar the way a browser places them; the shim
  // leaves their own connection to the test, as every section proof does.
  const section = findAll(el, (node) => node.tag === 'rdio-admin-systems')[0];

  assert.ok(section, 'the shell did not place the systems section on the page');

  section.connectedCallback();

  const bar = findAll(el, (node) => node.tag === 'rdio-admin-section-bar')[0];

  assert.ok(bar, 'the section did not carry its save bar');

  bar.connectedCallback();

  return el;
}

function region(el) {
  return findAll(el, (node) => node.tag === 'rdio-live-region')[0] ?? null;
}

function said(el) {
  return region(el)?.textContent.trim() ?? '';
}

function navButton(name) {
  return findAll(document.body, (node) => node.tag === 'button' && node.dataset.section === name)[0];
}

function saveBar() {
  return findAll(document.body, (node) => node.tag === 'rdio-admin-section-bar')[0];
}

function moveButton(rowLabel) {
  return findAll(document.body, (node) =>
    node.tag === 'button' && (node.getAttribute('aria-label') ?? '').startsWith('Move down — ') &&
    (node.getAttribute('aria-label') ?? '').includes(rowLabel))[0];
}

describe('administration announcements — the page says what changed', () => {
  it('carries a polite live region, empty of anything the page shows all the time', () => {
    const el = shell();
    const live = region(el);

    assert.ok(live, 'the administration page carries no live region for the operator who cannot see it');
    assert.equal(live.getAttribute('aria-live'), 'polite', 'the region interrupts the operator');
    assert.equal(live.getAttribute('role'), 'status');
    assert.equal(said(el), '', `the region carries static content: ${said(el)}`);
  });

  it('says the save the server answered', async () => {
    const el = shell();

    getAdminContext().useCases.saveSection = async () => true;

    const save = findAll(saveBar(), (node) => node.tag === 'button' && node.className?.includes('save'))[0];

    assert.ok(save, 'the section offers no save bar to answer the rows');

    save.click();

    await new Promise((resolve) => setImmediate(resolve));

    assert.equal(said(el), 'Saved', 'the page did not say the server took the rows');
  });

  it('says the save the server refused, naming the section', async () => {
    const el = shell();

    getAdminContext().useCases.saveSection = async () => false;

    const save = findAll(saveBar(), (node) => node.tag === 'button' && node.className?.includes('save'))[0];

    save.click();

    await new Promise((resolve) => setImmediate(resolve));

    const text = said(el);

    assert.match(text, /^Save failed: /, `the page did not say the save failed: ${JSON.stringify(text)}`);
    assert.match(text, /systems/i, 'the page did not name the section whose save failed');
  });

  it('names the row that moved and the place it landed on', () => {
    const el = shell();

    const button = moveButton('Metro');

    assert.ok(button, 'the row offers no control the keyboard moves it with');

    button.click();

    assert.equal(said(el), 'Metro, position 2 of 3',
      `the page did not say the row and its new place: ${JSON.stringify(said(el))}`);
  });
});
