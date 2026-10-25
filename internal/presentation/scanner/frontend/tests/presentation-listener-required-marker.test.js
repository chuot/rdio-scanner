// the listener shares the form field primitive with the administration
// page and declares nothing: the unlock code panel and the search
// filters ask for no field the listener must fill, so nothing on these
// surfaces carries the mark or the answering attribute.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { getAppContext } from '../application/context.js';

import '../../../common/frontend/presentation/ui/primitives.js';

import '../presentation/listener/rdio-display.js';
import '../presentation/listener/rdio-search-panel.js';

import { connectFields, findAll } from './dom-shim.js';

// The display runs a clock and a progress tick; a component test hands it the store it reads and
// disconnects it when the reading is taken, so no timer outlives the case.
function display() {
  const context = getAppContext();

  const el = document.createElement('rdio-display');

  el.connectedCallback();
  connectFields(el);

  return {
    el,
    store: context.store,
    close: () => el.disconnectedCallback(),
  };
}

function searchPanel() {
  const el = document.createElement('rdio-search-panel');

  el.connectedCallback();
  connectFields(el);

  return el;
}

// the mark a section declares, and the attribute the primitive stamps
// from it, both read straight off the page: the listener's stylesheet
// carries no rule drawing a mark, and its fields nothing to draw on.
function marks(root) {
  return findAll(root, (node) => node.getAttribute?.('data-required') === 'true');
}

function requiredControls(root) {
  return findAll(root, (node) => node.getAttribute?.('aria-required') === 'true');
}

describe('presentation/listener — the shared form field declares nothing here', () => {
  it('the unlock code panel carries no mark and no required control', () => {
    const { el, store, close } = display();

    try {
      store.setState({ auth: { status: 'pin-required', denied: false } });

      assert.deepEqual(marks(el).map((node) => node.textContent), [], 'no name on the unlock code panel carries the mark');
      assert.deepEqual(requiredControls(el), [], 'no control on the unlock code panel announces itself required');
    } finally {
      close();
    }
  });

  it('the search filters carry no mark and no required control, whatever the listener types in them', () => {
    const el = searchPanel();

    // A date field keeps only what a date field can hold: a browser refuses the text outright and
    // leaves the field empty, so the value the listener can leave in it is a date.
    const typedIn = (control) => (control.tag === 'input' && control.attributes.type === 'datetime-local' ? '2026-10-06T09:15' : 'fire');

    for (const control of findAll(el, (node) => ['input', 'textarea', 'select'].includes(node.tag))) {
      control.value = typedIn(control);

      // The panel reads the control the change came from, the way the browser reports it.
      control.dispatchEvent({ type: 'input', target: control });
      control.dispatchEvent({ type: 'change', target: control });
    }

    connectFields(el);

    assert.deepEqual(marks(el).map((node) => node.textContent), [], 'no name on the search filters carries the mark');
    assert.deepEqual(requiredControls(el), [], 'no control on the search filters announces itself required');
  });
});
