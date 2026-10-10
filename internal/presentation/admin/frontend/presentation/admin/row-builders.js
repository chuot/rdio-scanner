// Shared row builders for the systems tree sections (FEAT-016): the systems
// section and the three per-level sections (sites, talkgroups, units) build the
// same panels, captions, fields and delete rows, so the builders live once.
// The openState map keeps a row's details open across re-renders, exactly as
// each section component did before.

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

export function formField(errorTargets, details, title, caption, control, error, typeList) {
  const row = document.createElement('div');
  row.className = 'row';

  row.append(captionBlock(title, caption, typeList));

  const field = document.createElement('rdio-form-field');

  const label = document.createElement('label');
  label.textContent = title;

  field.append(label, control);

  if (error) errorTargets.push({ field, error });

  row.append(field);
  details.append(row);

  return field;
}

export function textField(errorTargets, details, title, caption, value, error) {
  const input = document.createElement('input');
  input.type = 'text';
  input.value = value ?? '';

  formField(errorTargets, details, title, caption, input, error);

  return { input };
}

export function numberField(errorTargets, details, title, caption, value, error) {
  const input = document.createElement('input');
  input.type = 'number';
  input.value = value === null || value === undefined ? '' : String(value);

  formField(errorTargets, details, title, caption, input, error);

  return { input };
}

export function textareaField(errorTargets, details, title, caption, value, error) {
  const textarea = document.createElement('textarea');
  textarea.value = value ?? '';

  formField(errorTargets, details, title, caption, textarea, error);

  return { textarea };
}

export function selectField(errorTargets, details, title, caption, options, value, onChange, typeList, error) {
  const select = document.createElement('rdio-select');
  select.setAttribute('options', JSON.stringify(options));
  select.setAttribute('value', value);
  select.addEventListener('change', (ev) => {
    // The primitive re-dispatches a CustomEvent with the string value; the
    // bubbled native change carries null (trusted) or undefined (synthetic)
    // detail and must be ignored, otherwise it clobbers the CustomEvent.
    if (typeof ev.detail === 'string') onChange(ev.detail);
  });

  formField(errorTargets, details, title, caption, select, error, typeList);

  return select;
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
