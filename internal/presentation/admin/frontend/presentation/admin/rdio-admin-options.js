// rdio-admin-options: options section (legacy config/options). Single
// object form (no rows): the editable options in legacy order with the legacy
// control types. The 12-hour time format, dimmer delay, duplicate detection
// time frame and playback-goes-live options are retired: the browser locale
// decides the time format, the dimmer delay and the duplicate window are
// domain constants, and playback always goes live. Edits clone the working
// object and set only the edited key, so server-only keys (adminPassword,
// secret) are preserved; per-section Save lives in the shared bar.

import { getAdminContext } from '../../application/admin-context.js';
import {
  bindSectionSlice,
  sectionSlice,
  sectionWorking,
  setSectionInvalid,
  setSectionWorking,
} from './section-shell.js';
import { clearReached, harvestReached, restoreReached } from './sub-section-shell.js';
import { markRequired, onFieldEdit } from './row-builders.js';

const NAME = 'options';

const NUMBER_FIELDS = [
  ['maxClients', 'Max clients', 1],
  ['pruneDays', 'Prune days', 0],
];

const CONTROLS = [
  {
    key: 'audioConversion',
    type: 'select',
    title: 'Audio Conversion',
    description: 'Convert incoming audio files to H.264 m4a with ffmpeg.',
    options: [
      [0, 'Disabled'],
      [1, 'Enabled without normalization'],
      [2, 'Enabled with normalization'],
      [3, 'Enabled with loud normalization'],
    ],
    numeric: true,
  },
  {
    key: 'autoPopulate',
    type: 'toggle',
    title: 'Auto Populate',
    description: 'Globally allows the automatic creation of unconfigured systems and talkgroups.',
  },
  {
    key: 'branding',
    type: 'text',
    title: 'Branding Label',
    description: 'A simple line of text displayed above the main screen to help identify the Rdio Scanner instance.',
  },
  {
    key: 'disableDuplicateDetection',
    type: 'toggle',
    title: 'Disable Duplicate Call Detection',
    description: 'Disable duplicate audio file detection.',
  },
  {
    key: 'email',
    type: 'text',
    title: 'Email Support',
    description: 'Email address where users can write to to get support.',
  },
  {
    key: 'keypadBeeps',
    type: 'select',
    title: 'Keypad Beep Style',
    description: 'Provide audio feedback when pressing buttons.',
    required: true,
    options: [
      ['uniden', 'Uniden'],
      ['whistler', 'Whistler'],
    ],
  },
  {
    key: 'maxClients',
    type: 'number',
    title: 'Max Clients',
    description: 'Max number of simultaneous clients.',
    required: true,
    min: 1,
  },
  {
    key: 'pruneDays',
    type: 'number',
    title: 'Prune Days',
    description: 'Prune the database for data older than the specified number of days. Set to 0 to disable.',
    required: true,
    min: 0,
  },
  {
    key: 'showListenersCount',
    type: 'toggle',
    title: 'Show Listeners Count',
    description: 'Show listeners count on main screen.',
  },
  {
    key: 'sortTalkgroups',
    type: 'toggle',
    title: 'Sort Talkgroups',
    description: 'Automatically sort talkgroups by their id.',
  },
];

export function optionsFieldErrors(options) {
  const errors = {};

  for (const [key, label, min] of NUMBER_FIELDS) {
    const raw = options?.[key];

    if (raw === undefined || raw === null || raw === '') {
      errors[key] = `${label} is required`;
      continue;
    }

    const value = Number(raw);

    if (!Number.isInteger(value) || value < min) errors[key] = `${label} is invalid`;
  }

  if (!options?.keypadBeeps) errors.keypadBeeps = 'Keypad beep style is required';

  return errors;
}

class RdioAdminOptions extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    this.unsubscribe = bindSectionSlice(NAME, () => this.render());

    this.unwatchFocus = getAdminContext().store.subscribe(['focus'], () => this.applyFocus());

    // The controls of this section's working copy remember the state of having been reached, the
    // way the systems tree sections do: the key they are remembered by names this section's slice.
    this.reached = new Set();
    this.rowKeyPrefix = `section:${NAME}`;

    this.render();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
    this.unwatchFocus?.();
  }

  render() {
    const slice = sectionSlice(NAME);

    if (!slice || slice.working === null || slice.working === this.internalEdit) return;

    harvestReached(this);

    // A section whose working copy matches the server's baseline — saved, discarded, or pushed from
    // the server — starts with every field unreached: the operator has touched nothing.
    if (!slice.dirty) clearReached(this, this.rowKeyPrefix);

    const options = slice.working;
    const errors = optionsFieldErrors(options);
    const errorFields = [];

    const root = document.createElement('div');
    root.className = 'rdio-admin-card options';

    // Every other section opens with an intro caption; Options names what it controls.
    const intro = document.createElement('p');
    intro.className = 'caption';
    intro.textContent = 'Server-wide options. Each control changes how the server behaves for every listener and caller.';

    root.append(intro);

    for (const control of CONTROLS) root.append(this.controlRow(control, options, errors, errorFields));

    const bar = document.createElement('rdio-admin-section-bar');
    bar.dataset.section = NAME;
    root.append(bar);

    this.replaceChildren(root);

    for (const { field, message } of errorFields) field.setError(message);

    restoreReached(this);

    this.applyFocus();
  }

  // The todo that named this control asked for it to be shown. The mark is applied
  // once, the request is consumed with it, and a control this section does not have
  // leaves the request untouched rather than marking nothing forever.
  applyFocus() {
    const { store, useCases } = getAdminContext();

    const request = store.getState().focus;

    if (request?.section !== NAME) return;

    const row = this.querySelector(`[data-control="${request.control}"]`);

    if (!row) return;

    row.className = 'row highlight';

    row.scrollIntoView?.({ block: 'center' });

    useCases.clearFocus();
  }

  controlRow(control, options, errors, errorFields) {
    const row = document.createElement('div');
    row.className = 'row';
    row.setAttribute('data-control', control.key);

    const description = document.createElement('p');
    description.innerHTML = '<span class="field-title"></span><br><span class="field-caption"></span>';
    description.querySelector('.field-title').textContent = control.title;
    description.querySelector('.field-caption').textContent = control.description;
    row.append(description);

    if (control.type === 'toggle') {
      const toggle = document.createElement('rdio-toggle');
      toggle.setAttribute('checked', String(!!options[control.key]));
      toggle.setAttribute('aria-label', control.title);
      toggle.addEventListener('change', (ev) => this.update(control.key, ev.detail));
      row.append(toggle);

      return row;
    }

    const field = document.createElement('rdio-form-field');

    // The caption is the name this row gives the control, so the marker rides it, and the field
    // host carries the other half of the pair so the control announces it.
    if (control.required) markRequired(field, description.querySelector('.field-title'));

    if (control.type === 'select') {
      const select = document.createElement('rdio-select');
      select.setAttribute('options', JSON.stringify(control.options));
      select.setAttribute('value', options[control.key] ?? '');
      select.setAttribute('aria-label', control.title);
      select.addEventListener('change', (ev) => {
        if (typeof ev.detail !== 'string') return;

        const value = control.numeric ? Number(ev.detail) : ev.detail;
        field.setError(this.update(control.key, value)[control.key] ?? '');
      });
      field.append(select);
    } else {
      const input = document.createElement('input');
      input.type = control.type;

      if (control.type === 'number') {
        input.min = String(control.min);
        input.step = '1';
        input.required = true;
      }

      input.value = options[control.key] ?? '';
      input.setAttribute('aria-label', control.title);
      onFieldEdit(input, () => {
        const value = control.type === 'number' ? (input.value === '' ? undefined : Number(input.value)) : input.value;
        field.setError(this.update(control.key, value)[control.key] ?? '');
      });
      field.append(input);
    }

    row.append(field);

    // The control carries the key this section remembers the state of having been reached by.
    field.dataset.field = `${this.rowKeyPrefix}:${control.key}`;

    if (errors[control.key]) errorFields.push({ field, message: errors[control.key] });

    return row;
  }

  // Preserve unknown keys: start from the working clone, set only this key.
  update(key, value) {
    const next = { ...sectionWorking(NAME), [key]: value };

    const errors = optionsFieldErrors(next);

    this.internalEdit = next;

    setSectionWorking(NAME, next);
    setSectionInvalid(NAME, Object.keys(errors).length > 0);

    return errors;
  }
}

customElements.define('rdio-admin-options', RdioAdminOptions);
