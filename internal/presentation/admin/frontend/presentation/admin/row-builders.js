// Shared row builders for the systems tree sections: the systems section and the three per-level
// sections (sites, talkgroups, units) build the same panels, captions, fields and delete rows, so
// the builders live once. The openState map keeps a row's details open across re-renders, exactly as
// each section component did before.
//
// A field carries the key its section remembers it by — the row's panel key and the field's name —
// so the section can hand the field back the state of having been reached when it rebuilds the row,
// the same way it hands back the open state.

export function makePanel(openState, key, title, invalid) {
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

export function formField(ctx, details, name, title, caption, control, error, typeList) {
  const row = document.createElement('div');
  row.className = 'row';

  row.append(captionBlock(title, caption, typeList));

  const field = document.createElement('rdio-form-field');

  const label = document.createElement('label');
  label.textContent = title;

  field.append(label, control);
  field.dataset.field = `${ctx.key}:${name}`;

  if (error) ctx.targets.push({ field, error });

  row.append(field);
  details.append(row);

  return field;
}

export function textField(ctx, details, name, title, caption, value, error) {
  const input = document.createElement('input');
  input.type = 'text';
  input.value = value ?? '';

  return { input, field: formField(ctx, details, name, title, caption, input, error) };
}

export function numberField(ctx, details, name, title, caption, value, error) {
  const input = document.createElement('input');
  input.type = 'number';
  input.value = value === null || value === undefined ? '' : String(value);

  return { input, field: formField(ctx, details, name, title, caption, input, error) };
}

export function textareaField(ctx, details, name, title, caption, value, error) {
  const textarea = document.createElement('textarea');
  textarea.value = value ?? '';

  return { textarea, field: formField(ctx, details, name, title, caption, textarea, error) };
}

export function selectField(ctx, details, name, title, caption, options, value, onChange, typeList, error) {
  const select = document.createElement('rdio-select');
  select.setAttribute('options', JSON.stringify(options));
  select.setAttribute('value', value);
  select.addEventListener('change', (ev) => {
    // The primitive re-dispatches a CustomEvent with the string value; the
    // bubbled native change carries null (trusted) or undefined (synthetic)
    // detail and must be ignored, otherwise it clobbers the CustomEvent.
    if (typeof ev.detail === 'string') onChange(ev.detail);
  });

  return { select, field: formField(ctx, details, name, title, caption, select, error, typeList) };
}

export function toggleField(details, title, caption, checked, onChange) {
  const toggle = document.createElement('rdio-toggle');
  toggle.checked = !!checked;
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

export function titleCase(value) {
  return value.replace(/[_-]/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

export function rowKey(row, index) {
  return typeof row.id === 'number' && row.id > 0 ? `id${row.id}` : `i${index}`;
}
