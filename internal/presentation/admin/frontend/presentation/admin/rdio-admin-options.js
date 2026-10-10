// rdio-admin-options (T041): options section (legacy config/options). Single
// object form (no rows): the editable options in legacy order with the legacy
// control types. The 12-hour time format, dimmer delay, duplicate detection
// time frame and playback-goes-live options are retired: the browser locale
// decides the time format, the dimmer delay and the duplicate window are
// domain constants, and playback always goes live. Edits clone the working
// object and set only the edited key, so server-only keys (adminPassword,
// secret) are preserved; per-section Save lives in the shared bar.

import {
  bindSectionSlice,
  sectionSlice,
  sectionWorking,
  setSectionInvalid,
  setSectionWorking,
} from './section-shell.js';

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
    min: 1,
  },
  {
    key: 'pruneDays',
    type: 'number',
    title: 'Prune Days',
    description: 'Prune the database for data older than the specified number of days. Set to 0 to disable.',
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

    this.render();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
  }

  render() {
    const slice = sectionSlice(NAME);

    if (!slice || slice.working === null || slice.working === this.internalEdit) return;

    const options = slice.working;
    const errors = optionsFieldErrors(options);
    const errorFields = [];

    const root = document.createElement('div');
    root.className = 'rdio-admin-card options';

    for (const control of CONTROLS) root.append(this.controlRow(control, options, errors, errorFields));

    const bar = document.createElement('rdio-admin-section-bar');
    bar.dataset.section = NAME;
    root.append(bar);

    this.replaceChildren(root);

    for (const { field, message } of errorFields) field.setError(message);
  }

  controlRow(control, options, errors, errorFields) {
    const row = document.createElement('div');
    row.className = 'row';

    const description = document.createElement('p');
    description.innerHTML = '<span class="field-title"></span><br><span class="field-caption"></span>';
    description.querySelector('.field-title').textContent = control.title;
    description.querySelector('.field-caption').textContent = control.description;
    row.append(description);

    if (control.type === 'toggle') {
      const toggle = document.createElement('rdio-toggle');
      toggle.setAttribute('checked', String(!!options[control.key]));
      toggle.addEventListener('change', (ev) => this.update(control.key, ev.detail));
      row.append(toggle);

      return row;
    }

    const field = document.createElement('rdio-form-field');

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
      input.addEventListener('change', () => {
        const value = control.type === 'number' ? (input.value === '' ? undefined : Number(input.value)) : input.value;
        field.setError(this.update(control.key, value)[control.key] ?? '');
      });
      field.append(input);
    }

    row.append(field);

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
