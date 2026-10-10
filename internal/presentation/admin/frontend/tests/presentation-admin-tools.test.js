// Tools panel layout and icons (FEAT-018, CA-003, CA-004): every panel icon is a
// valid Material Icons ligature, and every panel body follows the standard row
// pattern: caption, row.top with its controls, row.bottom with its action button.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../presentation/admin/rdio-admin-tools.js';

import { findAll, texts } from './dom-shim.js';

const VALID_ICONS = ['sync_alt', 'group_work', 'directions_car', 'lock', 'upload_file', 'delete'];

function toolsSection() {
  const { store } = getAdminContext();

  store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    'section:systems': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:groups': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:tags': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:access': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:apikeys': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:dirwatch': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:downstreams': { baseline: [], working: [], dirty: false, invalid: false, conflict: false },
    'section:options': { baseline: null, working: null, dirty: false, invalid: false, conflict: false },
    alerts: {},
  });

  const el = new (customElements.get('rdio-admin-tools'))();

  el.connectedCallback();

  return el;
}

function panelByTitle(el, title) {
  return findAll(el, (node) => node.tag === 'details').find(
    (panel) => texts(findAll(panel, (node) => node.className === 'row-title'))[0] === title,
  );
}

function rows(panel, cls) {
  return findAll(panel, (node) => node.tag === 'div' && node.className === cls);
}

// The components set input.type as a property, not as an attribute, so the shim
// sees it on the node itself.
const isFileInput = (node) => node.tag === 'input' && (node.attributes.type ?? node.type) === 'file';

test('every Tools panel icon is a valid Material Icons ligature', () => {
  const el = toolsSection();

  const icons = findAll(el, (node) => node.className === 'material-icons');

  assert.ok(icons.length >= 4, 'the Tools card has no panel icons');

  for (const icon of icons) {
    assert.ok(VALID_ICONS.includes(icon.textContent), `the Tools card shows ${icon.textContent}, not a Material Icons ligature`);
  }
});

test('the config panel follows the row pattern', () => {
  const panel = panelByTitle(toolsSection(), 'Import/export config');

  assert.ok(panel, 'Tools has no Import/export config panel');

  assert.equal(findAll(panel, (node) => node.className === 'caption').length, 1, 'the config panel has no caption');

  const top = rows(panel, 'row top');

  assert.equal(top.length, 1, 'the config panel has no row.top');

  // Export button and the styled Choose file button sit in the row; the native
  // picker is hidden and opens from the button.
  assert.equal(findAll(top[0], (node) => node.tag === 'button').length, 2, 'the config row.top has no export and choose-file buttons');
  assert.equal(findAll(top[0], (node) => node.className === 'file-name').length, 1, 'the config row.top has no chosen-file caption');
  assert.equal(findAll(panel, isFileInput).length, 1, 'the config panel has no file input');
  assert.equal(findAll(panel, isFileInput)[0].hidden, true, 'the native file picker must stay hidden');
});

test('the csv panels follow the row pattern with an action row', () => {
  const el = toolsSection();

  for (const title of ['Import talkgroups', 'Import units']) {
    const panel = panelByTitle(el, title);

    assert.ok(panel, `Tools has no ${title} panel`);

    assert.equal(findAll(panel, (node) => node.className === 'caption').length, 1, `${title} has no caption`);

    const top = rows(panel, 'row top');

    assert.equal(top.length, 1, `${title} has no row.top`);
    assert.equal(findAll(top[0], (node) => node.tag === 'rdio-select').length, 1, `${title} row.top has no system select`);
    assert.equal(findAll(top[0], (node) => node.tag === 'button').length, 1, `${title} row.top has no choose-file button`);
    assert.equal(findAll(top[0], (node) => node.className === 'file-name').length, 1, `${title} row.top has no chosen-file caption`);
    assert.equal(findAll(panel, isFileInput).length, 1, `${title} panel has no file input`);
    assert.equal(findAll(panel, isFileInput)[0].hidden, true, `${title} native file picker must stay hidden`);

    const bottom = rows(panel, 'row bottom');

    assert.equal(bottom.length, 1, `${title} has no row.bottom`);
    assert.equal(findAll(bottom[0], (node) => node.tag === 'button').length, 1, `${title} row.bottom has no import button`);
  }
});
