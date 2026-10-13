// The rows a section commits, and the moment it commits them.
//
// A field edit is committed with the marker that keeps the rows under the operator's hands, so the
// render that sees its own commit returns without rebuilding them. The drag handler was installed by
// the render that built the rows on the page: the move must commit the rows as the store holds them at
// the moment of the drop, and the section's validity must answer the same rows.
//
// The readings are taken from the rendered rows and from the section's working copy.

import './dom-shim.js';

import assert from 'node:assert/strict';
import test from 'node:test';

import { getAdminContext } from '../application/admin-context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/admin/rdio-admin-systems.js';
import '../presentation/admin/rdio-admin-groups.js';
import '../presentation/admin/rdio-admin-tags.js';
import '../presentation/admin/rdio-admin-dirwatch.js';

import { connectFields, fieldMessage, findAll, texts } from './dom-shim.js';

const METRO = { id: 1, label: 'Metro', order: 2, systemRef: 1, blacklists: '', sites: [], talkgroups: [], units: [] };
const COUNTY = { id: 2, label: 'County', order: 1, systemRef: 2, blacklists: '', sites: [], talkgroups: [], units: [] };

const GROUPS = [{ id: 1, order: 1, label: 'Dispatch', alert: '', led: '' }];
const TAGS = [{ id: 3, order: 1, label: 'Fire', alert: '', led: '' }];
const DIRWATCH = [{ id: 1, order: 1, directory: '/var/spool/first', extension: 'mp3', mask: '#SYS #TG', type: 'default', systemId: 2, siteId: null, talkgroupId: null, frequency: 1, delay: 2000, disabled: false }];

function slice(baseline) {
  return { baseline, working: structuredClone(baseline), dirty: false, invalid: false, conflict: false };
}

function seed(store) {
  store.setState({
    auth: { authenticated: true, docker: false, passwordNeedChange: false },
    alerts: {},
    'section:systems': slice([METRO, COUNTY]),
    'section:groups': slice(GROUPS),
    'section:tags': slice(TAGS),
    'section:dirwatch': slice(DIRWATCH),
  });

  return store;
}

function attach(tag) {
  const el = new (customElements.get(tag))();

  el.connectedCallback();
  connectFields(el);

  return el;
}

function control(el, className) {
  return findAll(el, (node) => node.tag === 'button' && node.className?.split(/\s+/).includes(className))[0];
}

function addRow(el) {
  control(el, 'add').click();
  connectFields(el);

  return rows(el)[0];
}

function rows(el) {
  return findAll(el, (node) => node.tag === 'div' && node.className?.split(/\s+/).includes('rdio-admin-row'));
}

function rowTitles(el) {
  return rows(el).map((row) => findAll(row, (node) => node.className?.split(/\s+/).includes('row-title'))[0]?.textContent);
}

function field(row, title) {
  const fields = findAll(row, (node) => node.tag === 'rdio-form-field');

  // The row sections with a dialog put a <label> inside the field; Groups, Tags, and Dirwatch name
  // the field in the description row beside it.
  return fields.find((node) => texts(findAll(node, (child) => child.tag === 'label'))[0] === title)
    ?? fields.find((node) => findAll(node.parent, (child) => child.className?.split(/\s+/).includes('field-title'))[0]?.textContent === title);
}

function controlOf(row, title) {
  return findAll(field(row, title), (node) => ['input', 'textarea', 'rdio-select'].includes(node.tag))[0];
}

function typeIn(row, title, value) {
  const control = controlOf(row, title);

  control.value = value;
  control.dispatchEvent({ type: 'change' });
  connectFields(row);

  return control;
}

function rowButton(row, label) {
  return findAll(row, (node) => node.tag === 'button' && node.textContent === label)[0];
}

function marker(row) {
  return findAll(row, (node) => node.className?.split(/\s+/).includes('row-invalid'))[0] ?? null;
}

function barButton(el, className) {
  const bar = findAll(el, (node) => node.tag === 'rdio-admin-section-bar')[0];

  bar.connectedCallback();

  return findAll(bar, (node) => node.tag === 'button' && node.className.split(/\s+/).includes(className))[0];
}

function saveButton(el) {
  return barButton(el, 'save');
}

function sectionWorking(name) {
  return getAdminContext().store.getState()[`section:${name}`].working;
}

// The drag handle's own events, fired on the list that carries the sort handlers.
function dragRow(el, from, to) {
  const list = findAll(el, (node) => node.className === 'rdio-admin-rows')[0];
  const moved = list.querySelectorAll(':scope > [data-drag]');
  const dataTransfer = {};
  const fire = (node, type) => list.dispatchEvent({ type, target: node, dataTransfer, preventDefault() {} });

  fire(moved[from], 'dragstart');
  fire(moved[to], 'dragover');
  fire(moved[to], 'drop');
  fire(moved[from], 'dragend');
}

// The row the operator came to fill in, the value they typed in it, and the move they made while it
// was still unsaved. The move must carry the row as the operator left it.
const DRAG_SECTIONS = [
  ['Groups', 'rdio-admin-groups', 'Label', 'Depot', 'Dispatch'],
  ['Tags', 'rdio-admin-tags', 'Label', 'Depot', 'Fire'],
  ['Dirwatch', 'rdio-admin-dirwatch', 'Directory', '/var/spool/new', '/var/spool/first'],
];

for (const [name, tag, title, typed, savedTitle] of DRAG_SECTIONS) {
  test(`dragging a ${name.toLowerCase()} row after editing it keeps what the operator typed`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);

    addRow(el);

    typeIn(rows(el)[0], title, typed);

    dragRow(el, 0, 1);

    assert.deepEqual(rowTitles(el), [savedTitle, typed],
      'the row the operator moved keeps the value they typed, and lands below the saved row');
  });

  test(`dragging a ${name.toLowerCase()} row commits the rows as the section holds them`, () => {
    seed(getAdminContext().store);

    const el = attach(tag);

    addRow(el);

    typeIn(rows(el)[0], title, typed);

    dragRow(el, 0, 1);

    const fieldOf = title === 'Label' ? 'label' : 'directory';

    assert.deepEqual(sectionWorking(name.toLowerCase()).map((row) => row[fieldOf]), [savedTitle, typed],
      'the working copy holds the rows in the order the operator left them, with the value they typed');
  });
}
