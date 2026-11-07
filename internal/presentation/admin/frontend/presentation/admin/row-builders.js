// Shared row builders for the admin webapp's row-list sections.
// openState keeps a row's details open across re-renders.

import { rowsToClose, scrollCompensation } from '../../domain/row-open-rule.js';

// Two buttons give the keyboard the drag handle's reorder. Ends
// disable the impossible move; attachDragSort owns the click.
export function rowMoveButtons(label, index, total) {
  const buttons = [];

  for (const [direction, glyph] of [['up', 'arrow_upward'], ['down', 'arrow_downward']]) {
    const button = document.createElement('button');

    button.type = 'button';
    button.className = 'rdio-icon-button row-move';
    button.dataset.move = direction;
    button.setAttribute('aria-label', `Move ${direction} — ${label}`);
    button.disabled = direction === 'up' ? index <= 0 : index >= total - 1;

    const icon = document.createElement('i');

    icon.className = 'material-icons';
    icon.setAttribute('aria-hidden', 'true');
    icon.textContent = glyph;

    button.append(icon);
    buttons.push(button);
  }

  return buttons;
}

// A renamed row's move buttons carry its new name: control text matches the page.
export function setRowMoveLabels(panel, label) {
  for (const button of panel.querySelectorAll('button[data-move]')) {
    button.setAttribute('aria-label', `Move ${button.dataset.move} — ${label}`);
  }
}

export function makePanel(openState, key, title, invalid, position = {}) {
  const details = document.createElement('details');
  details.className = 'rdio-admin-row';
  details.dataset.key = key;
  details.dataset.drag = '';
  details.draggable = true;
  details.open = openState.get(key) ?? false;

  const summary = document.createElement('summary');

  const handle = document.createElement('i');
  handle.className = 'material-icons drag-handle';
  handle.setAttribute('aria-hidden', 'true');
  handle.textContent = 'drag_indicator';
  summary.append(handle);

  const titleSpan = document.createElement('span');
  titleSpan.className = 'row-title';
  titleSpan.textContent = title;
  summary.append(titleSpan);

  summary.append(...rowMoveButtons(title, position.index ?? 0, position.total ?? 1));

  if (invalid) {
    const icon = document.createElement('i');
    icon.className = 'material-icons row-invalid';
    icon.setAttribute('aria-label', 'Invalid row');
    icon.textContent = 'error';
    summary.append(icon);
  }

  details.append(summary);

  return details;
}

// A NodeList has no filter, so the open rows are put in an array first.
// The rows open on the page, in list order; the section's map follows them.
// The invalid-row icon rides the row the operator edits, moved on the spot.
function openRowsOf(list) {
  return Array.from(list.querySelectorAll(':scope > details[data-key]')).filter((row) => row.open);
}

function openKeysOf(list) {
  return openRowsOf(list).map((row) => row.dataset.key);
}

// scrollableAncestor walks up to the first element that scrolls, or null.
function scrollableAncestor(list) {
  const scrolls = (el) => {
    if (typeof el.scrollHeight !== 'number' || typeof el.clientHeight !== 'number') return false;
    if (el.scrollHeight <= el.clientHeight) return false;

    const overflow = typeof getComputedStyle === 'function' ? getComputedStyle(el).overflowY : 'visible';

    return overflow === 'auto' || overflow === 'scroll';
  };

  for (let el = list?.parentElement ?? list?.parent; el; el = el.parentElement ?? el.parent) {
    if (scrolls(el)) return el;
  }

  return null;
}

// One open row at a time: toggle fires on the row and never bubbles. openState
// follows the page; scroll compensation keeps the opened row in view.
export function attachSingleOpenRule(list, openState) {
  let applying = false;

  const onActivation = (ev) => {
    const row = ev.target?.closest?.('details[data-key]');

    if (!row || applying) return;

    const key = row.dataset.key;

    if (!row.open) {
      openState.set(key, false);

      return;
    }

    const scroller = scrollableAncestor(list);
    const before = scroller ? row.getBoundingClientRect().top : Number.NaN;

    const open = openRowsOf(list);

    applying = true;

    for (const other of rowsToClose(openKeysOf(list), key)) {
      const sibling = open.find((candidate) => candidate.dataset.key === other);

      if (sibling) sibling.open = false;

      openState.set(other, false);
    }

    openState.set(key, true);

    applying = false;

    if (scroller) scroller.scrollTop += scrollCompensation(before, row.getBoundingClientRect().top);
  };

  for (const row of Array.from(list.querySelectorAll(':scope > details[data-key]'))) {
    row.addEventListener('toggle', onActivation);
  }
}

export function setRowMarker(panel, invalid) {
  const icon = panel.querySelectorAll('.row-invalid')[0];

  if (invalid && !icon) {
    const added = document.createElement('i');
    added.className = 'material-icons row-invalid';
    added.setAttribute('aria-label', 'Invalid row');
    added.textContent = 'error';

    panel.querySelector('summary')?.append(added);
  }

  if (!invalid && icon) icon.remove();
}

export function captionBlock(title, caption, typeList) {
  const help = document.createElement('p');

  const body = document.createElement('span');
  body.className = 'body';
  body.textContent = title;

  help.append(body, document.createElement('br'));

  if (caption) {
    const captionEl = document.createElement('span');
    captionEl.className = 'caption';

    // The caption is text, or nodes with inline emphasis (the dirwatch mask tokens).
    // Assigning an array to textContent renders "[object Text]".
    if (typeof caption === 'string') captionEl.textContent = caption;
    else captionEl.append(...caption);

    help.append(captionEl);
  }

  if (typeList) {
    const list = document.createElement('ul');

    for (const [name, description] of typeList) {
      const item = document.createElement('li');

      const bold = document.createElement('b');
      bold.textContent = name;

      item.append(bold, ' - ', document.createTextNode(description));
      list.append(item);
    }

    help.append(list);
  }

  return help;
}

// Field context: error targets, reached state, key from slice plus row panel key.
export function rowContext(component, key) {
  const prefix = component.rowKeyPrefix ? `${component.rowKeyPrefix}:` : '';

  return { targets: component.errorTargets, reached: component.reached, key: `${prefix}${key}` };
}

// Hand-built fields take the same key, so the section finds the node to restore.
export function markField(component, field, key, name) {
  const prefix = component.rowKeyPrefix ? `${component.rowKeyPrefix}:` : '';

  field.dataset.field = `${prefix}${key}:${name}`;

  return field;
}

export function formField(ctx, details, name, title, caption, control, error, typeList, required) {
  const row = document.createElement('div');
  row.className = 'row';

  row.append(captionBlock(title, caption, typeList));

  const field = document.createElement('rdio-form-field');

  const label = document.createElement('label');
  label.textContent = title;

  field.append(label, control);
  field.dataset.field = `${ctx.key}:${name}`;

  // The asterisk mark rides the field's own label and the field host, set at build.
  if (required) markRequired(field, label);

  if (error) ctx.targets.push({ field, error });

  row.append(field);
  details.append(row);

  return field;
}

// A field named by a caption outside it (options panel, Groups list): the mark
// rides that caption, and the field host carries it for the announcement.
export function markRequired(field, namingEl) {
  field.dataset.required = 'true';
  namingEl.dataset.required = 'true';
}

export function textField(ctx, details, name, title, caption, value, error, required) {
  const input = document.createElement('input');
  input.type = 'text';
  input.value = value ?? '';

  return { input, field: formField(ctx, details, name, title, caption, input, error, null, required) };
}

export function numberField(ctx, details, name, title, caption, value, error, required) {
  const input = document.createElement('input');
  input.type = 'number';
  input.value = value === null || value === undefined ? '' : String(value);

  return { input, field: formField(ctx, details, name, title, caption, input, error, null, required) };
}

export function textareaField(ctx, details, name, title, caption, value, error, required) {
  const textarea = document.createElement('textarea');
  textarea.value = value ?? '';

  return { textarea, field: formField(ctx, details, name, title, caption, textarea, error, null, required) };
}

export function selectField(ctx, details, name, title, caption, options, value, onChange, typeList, error, required) {
  const select = document.createElement('rdio-select');
  select.setAttribute('options', JSON.stringify(options));
  select.setAttribute('value', value);
  select.addEventListener('change', (ev) => {
    // Only the CustomEvent re-dispatched with a string value counts: the bubbled
    // native change carries null/undefined detail and would clobber it.
    if (typeof ev.detail === 'string') onChange(ev.detail);
  });

  return { select, field: formField(ctx, details, name, title, caption, select, error, typeList, required) };
}

export function toggleField(details, title, caption, checked, onChange) {
  const toggle = document.createElement('rdio-toggle');
  toggle.checked = !!checked;
  // The switch's name is the caption block's title beside it: aria-label puts
  // the name on the control that holds the switch role.
  toggle.setAttribute('aria-label', title);
  toggle.addEventListener('change', (ev) => onChange(ev.detail));

  const row = document.createElement('div');
  row.className = 'row';

  row.append(captionBlock(title, caption), toggle);
  details.append(row);

  return toggle;
}

export function deleteRow(details, label, onClick, container) {
  const bottom = container ?? document.createElement('div');
  bottom.className = 'row bottom';

  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'rdio-button-mini delete warn';
  button.textContent = label;
  button.addEventListener('click', onClick);

  bottom.append(button);
  details.append(bottom);
}

export function parseNumber(value) {
  return value === '' || value === null ? null : Number(value);
}

// Recompute the field's message on the keystroke, not when leaving.
// A leave-time message describes the value the keystroke typed over.
// On the control, the handler runs before the field's own reach listener.
export function onFieldEdit(control, handler) {
  for (const type of ['input', 'change']) {
    control.addEventListener(type, handler);
  }
}

export function titleCase(value) {
  return value.replace(/[_-]/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

// ROW_IDENTITY: a Symbol marks added unsaved rows; spreads keep symbol keys.
// Structured clone, JSON, and the store's comparison read string keys only.
// A saved payload never carries it; the mark alone is not a change.
export const ROW_IDENTITY = Symbol('rdio-admin-row-identity');

let nextRowIdentity = 1;

// The row the add control creates takes its mark as it is created, so the row is itself from its
// first moment: the position a later render happens to leave it in is not what it is remembered by.
export function markRowIdentity(row) {
  row[ROW_IDENTITY] = nextRowIdentity++;

  return row;
}

export function rowIdentity(row) {
  return row[ROW_IDENTITY] ?? null;
}

export function rowKey(row, index) {
  if (typeof row.id === 'number' && row.id > 0) return `id${row.id}`;

  const identity = rowIdentity(row);

  return identity === null ? `i${index}` : `new${identity}`;
}
