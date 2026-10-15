// The listener's unlock code panel and the search filters, read through the same form field the
// administration sections use: the panel tells the listener why the code was refused even when they
// never left the field — they pasted it, the browser filled it in, they pressed Enter — and a panel
// nobody has typed in says nothing. The search filters validate nothing, so they carry no message at
// all, whatever the listener does with them.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { getAppContext } from '../application/context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

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
      // The unlock code is user-specific: no generic autofill purpose describes it, so the
      // field suppresses the browser's purpose (the SC 1.3.5 exception) and carries the words
      // the panel shows on it as its name.
      assert.equal(input.getAttribute('aria-label'), 'Unlock code');
      assert.equal(input.getAttribute('autocomplete'), 'off');
    } finally {
      close();
    }
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
