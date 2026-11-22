// the listener's unlock code panel and search filters, read through
// the shared form field primitive: the panel tells the listener why
// the code was refused even when they never left the field, and a
// panel nobody typed in says nothing. the filters validate nothing.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { getAppContext } from '../application/context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import { PIN_STORAGE_KEY } from '../domain/pin.js';

import '../presentation/listener/rdio-display.js';
import '../presentation/listener/rdio-search-panel.js';

import { connectFields, fieldMessage, findAll } from './dom-shim.js';

// The display runs a clock and a progress tick; a component test hands it the store it reads and
// disconnects it when the reading is taken, so no timer outlives the case.
function display() {
  const context = getAppContext();
  const { store } = context;

  const el = document.createElement('rdio-display');

  el.connectedCallback();
  connectFields(el);

  return {
    el,
    store,
    storage: context.storage,
    useCases: context.useCases,
    field: el.querySelector('rdio-form-field'),
    input: el.querySelector('#unlock-code'),
    form: el.querySelector('.auth form'),
    close: () => el.disconnectedCallback(),
  };
}

function searchPanel() {
  const el = document.createElement('rdio-search-panel');

  el.connectedCallback();
  connectFields(el);

  return el;
}

describe('presentation/listener/rdio-display — the unlock code panel', () => {
  it('says nothing to a listener who has not typed in the unlock code field', () => {
    const { store, field, close } = display();

    try {
      store.setState({ auth: { status: 'pin-required', denied: false } });

      assert.equal(fieldMessage(field), '', 'the panel the listener has not touched writes nothing under the code field');
    } finally {
      close();
    }
  });

  it('names a refused code on the field, when the code arrived without the listener leaving it', () => {
    const { store, useCases, field, input, form, close } = display();

    try {
      // The browser filled the field in: the value is there and no event ever left the control,
      // so the field is one the listener has not reached by typing.
      input.value = '1234';

      // The server's answer to the code the panel sent.
      useCases.authenticate = () => store.setState({ auth: { status: 'pin-required', denied: true } });

      form.dispatchEvent({ type: 'submit', preventDefault: () => {} });

      assert.equal(fieldMessage(field), 'Invalid unlock code', 'submitting the form reaches the field it submits');
    } finally {
      close();
    }
  });

  it('empties the unlock field once the server has accepted the code typed into it', () => {
    const { store, storage, useCases, input, form, close } = display();

    try {
      store.setState({ auth: { status: 'pin-required', denied: false } });
      useCases.authenticate = () => store.setState({ auth: { status: 'authenticated', denied: false } });

      input.value = '1234';
      form.dispatchEvent({ type: 'submit', preventDefault: () => {} });

      // The code has done its work: the next time the screen comes back it is a fresh request, and
      // a field still carrying the code that worked is not one.
      assert.equal(input.value, '', 'an accepted code leaves the field it was typed into');
    } finally {
      storage.local.removeItem(PIN_STORAGE_KEY);
      close();
    }
  });

  it('shows a fresh empty request when the code the page held was refused', () => {
    const { store, storage, useCases, field, input, form, close } = display();

    try {
      // The listener unlocked earlier in this session, and that code was accepted.
      store.setState({ auth: { status: 'pin-required', denied: false } });
      useCases.authenticate = () => store.setState({ auth: { status: 'authenticated', denied: false } });

      input.value = '1234';
      form.dispatchEvent({ type: 'submit', preventDefault: () => {} });

      // A later ask is answered by the page from what it holds, and the server refuses that answer.
      store.setState({ auth: { status: 'pin-required', denied: true } });

      assert.equal(fieldMessage(field), '', 'a refusal of a code nobody typed writes nothing under the field');
      assert.equal(input.value, '', 'the request a refused code brings back is empty');
    } finally {
      storage.local.removeItem(PIN_STORAGE_KEY);
      close();
    }
  });

  it('names an expired code on the field once the listener has typed in it', () => {
    const { store, field, input, close } = display();

    try {
      input.value = '1234';
      input.dispatchEvent({ type: 'input' });

      store.setState({ auth: { status: 'expired', denied: false } });

      assert.equal(fieldMessage(field), 'This unlock code has expired');
    } finally {
      close();
    }
  });

  it('carries the unlock field\'s name and the suppression its purpose asks for', () => {
    const { input, close } = display();

    try {
      // the unlock code is user-specific: no generic autofill purpose
      // describes it, so the field suppresses the browser's purpose, an
      // identified-autofill exception, and carries the panel's words.
      assert.equal(input.getAttribute('aria-label'), 'Unlock code');
      assert.equal(input.getAttribute('autocomplete'), 'off');
    } finally {
      close();
    }
  });
});

describe('rdio-toggle — the name rides the switch the operator reaches', () => {
  it('moves the name the page puts on the host onto the control that carries the switch role', () => {
    const el = document.createElement('rdio-toggle');

    el.setAttribute('aria-label', 'Download mode');
    el.connectedCallback();

    const button = el.querySelector('button.rdio-toggle');

    assert.equal(button.getAttribute('role'), 'switch');
    assert.equal(button.getAttribute('aria-label'), 'Download mode',
      'the switch the operator reaches carries the name the page put on its host');
    assert.equal(el.getAttribute('aria-label'), null, 'the host keeps no duplicate name');
  });
});

describe('presentation/listener/rdio-search-panel — the filters carry no messages', () => {
  const filterFields = (el) => findAll(el, (node) => node.tag === 'rdio-form-field');

  it('opens with every filter field silent', () => {
    const el = searchPanel();

    assert.deepEqual(
      filterFields(el).map(fieldMessage).filter(Boolean),
      [],
      'a filter panel validates nothing, so it writes nothing under any control',
    );
  });

  it('says nothing about a filter the listener reached, because a filter has no message to reveal', () => {
    const el = searchPanel();

    // markReached is the reading the panel's own submit would hand a field: the field is one the
    // listener has touched. A filter carries no validation, so touching it reveals nothing.
    for (const field of filterFields(el)) field.markReached();

    assert.deepEqual(
      filterFields(el).map(fieldMessage).filter(Boolean),
      [],
      'reaching a filter reveals no message',
    );
  });
});
