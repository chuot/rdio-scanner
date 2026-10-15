// Shared row builders for the systems tree sections: the systems section and the three per-level
// sections (sites, talkgroups, units) build the same panels, captions, fields and delete rows, so
// the builders live once. The openState map keeps a row's details open across re-renders, exactly as
// each section component did before.
//
// A field carries the key its section remembers it by — the row's panel key and the field's name —
// so the section can hand the field back the state of having been reached when it rebuilds the row,
// the same way it hands back the open state.

import { rowsToClose, scrollCompensation } from '../../domain/row-open-rule.js';

// The keyboard's route to a row's place in the list.
//
// The row's position is set by dragging its handle: a gesture the pointer owns. Two buttons beside
// the handle give the same move to the keyboard — and to an operator who cannot hold a pointer steady
// through a drag — and they go through the same reorder the drag goes through, so the save bar sends
// the same order either way.
//
// The buttons name the row they move, in the row's own words: "Move up — Metro". A row the operator
// renames takes its new name, because the name on the control is the name on the page.
//
// The ends of the list have nowhere to move: the first row's Move up and the last row's Move down are
// disabled, so the keyboard is never left on a control that does nothing.
//
// The buttons sit inside the row's own control — the summary line that opens and closes the row — so
// the click that moves the row has to take the row's activation away from the browser, which the
// shared wiring in attachDragSort does.
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

// The row the operator renamed carries its new name on the controls that move it: the name on the
// control is the name on the page, and a control that names a row the operator has just renamed says
// something the screen does not show.
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

// The row's warning icon names the row invalid, and follows the row's validity: it is added when
// the row gains an error and taken away when it loses its last one. The row the operator is editing
// stays on the page, so the icon is moved on the spot rather than rebuilt.
// The rows the operator sees open, in the order the list presents them. The page is the state the
// operator reads; the section's map is what reproduces it on the next render, so the rule reads the
// rows off the list and keeps the map in step with it.
//
// A browser hands back a NodeList from a query: it carries its rows by index, `forEach`, and
// iteration, and it carries no `filter`. The rows are put in an array before they are picked over.
function openRowsOf(list) {
  return Array.from(list.querySelectorAll(':scope > details[data-key]')).filter((row) => row.open);
}

function openKeysOf(list) {
  return openRowsOf(list).map((row) => row.dataset.key);
}

// The container the operator scrolls: the administration content area, the element that moves the rows
// when the content above them changes. Walking up from the list to the first element that actually
// scrolls answers nothing when nothing scrolls — outside a browser, and on a page that does not move.
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

// One row open at a time. The systems-tree sections mount this on the rows of the list they build in
// a render, so one handler covers every row the section renders, the rows added later included.
//
// The handler answers the toggle event a summary activation produces — the click and the keyboard's
// Enter and Space leave the same event — and it answers it on the row, because the browser leaves
// that event on the row the operator activated and does not bubble it up to the list. Opening a row
// closes the rows that were open; closing a row records its own state and closes nothing. The
// section's map follows the page, so the next render reproduces the one open row the operator is
// looking at.
//
// Nothing here touches the store: reading a row is not a change, and the rows the operator is editing
// stay on the page. The rows above the opened row are the content that disappears, so the section
// measures how far the opened row moved and gives that distance back to the container the operator
// scrolls, which keeps the row they clicked under their eyes.
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

    // A caption is plain text, or a list of nodes with inline emphasis (the dirwatch
    // mask tokens); assigning an array to textContent renders "[object Text]" literals.
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

// The context a row's fields are built with: where the field hands its message over, the reached
// state the section carries across its rebuilds, and the key the field's own key is built on — the
// slice the section is showing, then the row's panel key, so a field is remembered by the row it
// belongs to and not by the position a render happens to leave it in.
export function rowContext(component, key) {
  const prefix = component.rowKeyPrefix ? `${component.rowKeyPrefix}:` : '';

  return { targets: component.errorTargets, reached: component.reached, key: `${prefix}${key}` };
}

// The sections that build their fields by hand mark them with the same key: the field is the node
// the section hands back the state of having been reached, and it finds it by this key.
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

  // The mark the operator reads as an asterisk is a declaration the section makes about the field,
  // stated here once when the field is built: it names the field's own label, and the field host
  // carries it too, so the primitive can announce the same fact on the control.
  if (required) markRequired(field, label);

  if (error) ctx.targets.push({ field, error });

  row.append(field);
  details.append(row);

  return field;
}

// The mark on a field whose name is not a label inside it. A hand-built row names its field with a
// caption built outside the field — the options panel, the Groups list — and the mark rides that
// caption, with the field host carrying it for the announcement on the control.
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
    // The primitive re-dispatches a CustomEvent with the string value; the
    // bubbled native change carries null (trusted) or undefined (synthetic)
    // detail and must be ignored, otherwise it clobbers the CustomEvent.
    if (typeof ev.detail === 'string') onChange(ev.detail);
  });

  return { select, field: formField(ctx, details, name, title, caption, select, error, typeList, required) };
}

export function toggleField(details, title, caption, checked, onChange) {
  const toggle = document.createElement('rdio-toggle');
  toggle.checked = !!checked;
  // The switch's name is the words the caption block shows beside it; the primitive carries the
  // name onto the control that holds the switch role.
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

// The message a field carries is recomputed on the keystroke, not only when the operator leaves.
//
// The form field reveals its message the moment the value in the control changes. A message computed
// when the operator leaves is therefore the message of the value the keystroke typed over: the first
// digit of a valid identifier shows the complaint the empty field earned. Attaching the section's
// commit to the keystroke as well makes the page answer the value the operator is typing.
//
// The handler sits on the control, so it runs before the form field's own reach listener on the field
// that owns the control, and the message is recomputed before the field paints.
export function onFieldEdit(control, handler) {
  for (const type of ['input', 'change']) {
    control.addEventListener(type, handler);
  }
}

export function titleCase(value) {
  return value.replace(/[_-]/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

// A row the operator added and has not saved is remembered by a mark the client gives it. The mark is
// the client's own bookkeeping: it rides the row through every copy the section makes (an object
// spread carries symbol-keyed properties), and it is invisible to everything that leaves the client —
// a structured clone, a JSON stringify, and the store's structural comparison all read string keys
// only. A saved payload therefore never carries it, and a row that differs from the server's copy by
// the mark alone is not a change.
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
