// Shared UI primitives: keyboard-operable custom elements replacing
// Angular Material. All are plain custom elements with Light DOM.

class RdioSnackbar extends HTMLElement {
  static show(message, duration = 5000) {
    let host = document.querySelector('rdio-snackbar');

    if (!host) {
      host = document.createElement('rdio-snackbar');
      document.body.appendChild(host);
    }

    host.render(message);

    if (host.timer) clearTimeout(host.timer);
    if (typeof duration === 'number') host.timer = setTimeout(() => host.render(''), duration);
  }

  connectedCallback() {
    this.addEventListener('animationend', (event) => this.animationEnd(event));
  }

  render(message) {
    this.setAttribute('role', 'status');

    if (!this.hasAttribute('material')) {
      this.hidden = !message;

      if (typeof message === 'string') this.textContent = message;
      else this.replaceChildren(message);

      return;
    }

    // The scanner asks for the shape the baseline has: a surface carrying the
    // colour, the shape, and the elevation, with the message in a label inside
    // it, and the 150ms enter and 75ms exit animations the baseline plays. The
    // administration application asks for nothing of the kind and renders as it
    // always did.
    const content = typeof message === 'string' ? document.createTextNode(message) : message;

    if (!message) {
      if (this.hidden || this.classList.contains('rdio-snackbar-exit')) return;

      this.classList.remove('rdio-snackbar-enter');
      this.classList.add('rdio-snackbar-exit');

      return;
    }

    const label = document.createElement('div');
    label.className = 'rdio-snackbar-label';
    label.append(content);

    const surface = document.createElement('div');
    surface.className = 'rdio-snackbar-surface';
    surface.append(label);

    this.hidden = false;
    this.replaceChildren(surface);
    this.classList.remove('rdio-snackbar-exit');
    this.classList.remove('rdio-snackbar-enter');
    // Reading the layout is what lets the same enter animation play again on a
    // host that has already played it once.
    void this.offsetWidth;
    this.classList.add('rdio-snackbar-enter');
  }

  animationEnd(event) {
    if (event.animationName !== 'rdio-snackbar-exit') return;

    this.classList.remove('rdio-snackbar-exit');
    this.hidden = true;
    this.replaceChildren();
  }
}

customElements.define('rdio-snackbar', RdioSnackbar);

export function showSnackbar(message, duration) {
  RdioSnackbar.show(message, duration);
}

export function dismissSnackbar() {
  const host = document.querySelector('rdio-snackbar');

  if (host) {
    clearTimeout(host.timer);
    host.render('');
  }
}

class RdioDialog extends HTMLElement {
  static open({ title, content, actions = [] }) {
    const dialog = document.createElement('rdio-dialog');

    document.body.appendChild(dialog);

    dialog.setup(title, content, actions);

    return dialog;
  }

  setup(title, content, actions) {
    this.setAttribute('role', 'dialog');
    this.setAttribute('aria-modal', 'true');
    this.setAttribute('aria-label', title);

    const box = document.createElement('div');
    box.className = 'rdio-dialog-box mat-elevation';

    const heading = document.createElement('h2');
    heading.textContent = title;
    box.append(heading);

    const body = document.createElement('div');
    body.className = 'rdio-dialog-body';
    if (typeof content === 'string') body.innerHTML = content;
    else body.append(content);
    box.append(body);

    const bar = document.createElement('div');
    bar.className = 'rdio-dialog-actions';

    for (const action of actions) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'rdio-button-mini';
      button.textContent = action.label;
      button.addEventListener('click', () => {
        this.close();
        action.onClick?.();
      });
      bar.append(button);
    }

    box.append(bar);
    this.append(box);

    this.previousFocus = document.activeElement;

    const focusables = this.querySelectorAll('button, input, select, textarea, [tabindex]');
    focusables[0]?.focus();

    this.keyHandler = (ev) => {
      if (ev.key === 'Escape') {
        this.close();
        return;
      }

      if (ev.key === 'Tab' && focusables.length) {
        const first = focusables[0];
        const last = focusables[focusables.length - 1];

        if (ev.shiftKey && document.activeElement === first) {
          ev.preventDefault();
          last.focus();
        } else if (!ev.shiftKey && document.activeElement === last) {
          ev.preventDefault();
          first.focus();
        }
      }
    };

    this.addEventListener('keydown', this.keyHandler);
  }

  close() {
    this.removeEventListener('keydown', this.keyHandler);
    this.remove();
    this.previousFocus?.focus?.();
  }
}

customElements.define('rdio-dialog', RdioDialog);

export function openDialog(options) {
  return RdioDialog.open(options);
}

class RdioFormField extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    const label = this.querySelector('label');

    const error = document.createElement('div');
    error.className = 'rdio-form-error';
    error.setAttribute('role', 'alert');
    this.append(error);

    // The message a section last computed for this field, and whether the operator has reached it.
    // A field is reached when the operator changes its value, picks in it, or the form holding it is
    // submitted — the reading Angular Material's error matcher used. Walking across a field says
    // nothing: an invalid value nobody has edited is not reported.
    let message = '';
    let reached = false;

    // The control is looked up when the field paints, not when it is connected: a select field's
    // inner select is built by its own connectedCallback, which runs after this one, so the control
    // is not under the field yet at connect time.
    const control = () => this.querySelector('input, select, textarea');

    // A section declares, when it builds the field, that the webapp insists on a value there. The
    // declaration is a fact about the field and not about its current state, and it is the fact the
    // operator reads as an asterisk on the field's name: the assistive technology reading the page
    // needs the same fact on the control it announces, so the control answers required while the
    // field declares it, and answers nothing at all when the field does not.
    const declares = () => this.dataset.required === 'true' || this.getAttribute('data-required') === 'true';

    const paint = () => {
      const shown = reached && !!message;
      const input = control();

      error.textContent = shown ? message : '';

      // `aria-invalid` answers true or false: an empty value is not a name the screen reader and the
      // stylesheet can read, so the mark is written as the word, and taken away when the field has
      // nothing to say.
      if (!input) return;

      if (shown) input.setAttribute('aria-invalid', 'true');
      else input.removeAttribute('aria-invalid');

      // The announcement is written the same way, and follows the declaration: a field the section
      // accepts empty leaves the control unannounced, and a field that insists on a value announces
      // it whatever the row's state happens to be.
      if (declares()) input.setAttribute('aria-required', 'true');
      else input.removeAttribute('aria-required');
    };

    this.setError = (text) => {
      message = text ?? '';
      paint();
    };

    this.markReached = () => {
      reached = true;
      paint();
    };

    this.isReached = () => reached;

    const field = control();

    if (label && field && !field.id) {
      field.id = `fld-${Math.random().toString(36).slice(2, 8)}`;
      label.htmlFor = field.id;
    }

    // Typing and picking bubble out of the control to the field that owns it: the value changing is
    // the reaching. Walking across a field changes nothing, and reveals nothing — Material's matcher
    // read a touched control as silent. A form submit reaches no control at all: the form that is
    // submitted tells its fields themselves, through markReached.
    for (const type of ['input', 'change']) {
      this.addEventListener(type, () => this.markReached());
    }

    // A pick list builds the select it answers with after this field is connected, so the field
    // paints once at connect, for the control that is already under it, and again when the control
    // a pick list builds appears: that select is the element the label is attached to and the
    // screen reader announces, and it is the one carrying the field's declarations.
    this.addEventListener('rdio-control', () => paint());

    paint();
  }
}

customElements.define('rdio-form-field', RdioFormField);

class RdioSelect extends HTMLElement {
  connectedCallback() {
    if (this.querySelector('select')) return;

    const select = document.createElement('select');

    for (const [value, label] of JSON.parse(this.getAttribute('options') ?? '[]')) {
      const option = document.createElement('option');
      option.value = value;
      option.textContent = label;
      select.append(option);
    }

    if (this.hasAttribute('value')) select.value = this.getAttribute('value');

    select.addEventListener('change', () => {
      // The value leaves the select as a bubbling event: the form field that owns the select is
      // listening for the change the operator made, and a non-bubbling event stops at this element.
      this.dispatchEvent(new CustomEvent('change', { detail: select.value, bubbles: true }));
    });

    this.append(select);
    this.select = select;

    // The select built here is the control the field owning this pick list resolves to, and the
    // element a label is attached to and a screen reader announces. It appears after the field is
    // connected, so the field is told the control it answers with is there, and paints what the
    // field declares onto it.
    this.dispatchEvent(new CustomEvent('rdio-control', { bubbles: true }));
  }

  set options(pairs) {
    const select = this.select;
    const current = this.getAttribute('value');

    select.innerHTML = '';

    for (const [value, label] of pairs) {
      const option = document.createElement('option');
      option.value = value;
      option.textContent = label;
      select.append(option);
    }

    if (pairs.some(([v]) => String(v) === String(current))) select.value = current;
  }

  get value() {
    return this.select?.value;
  }

  set value(v) {
    if (this.select) this.select.value = v;
  }
}

customElements.define('rdio-select', RdioSelect);

class RdioToggle extends HTMLElement {
  connectedCallback() {
    const button = document.createElement('button');

    button.type = 'button';
    button.className = 'rdio-toggle';
    button.setAttribute('role', 'switch');

    // The listener's toggle is the legacy slide toggle: the icon of the mode it
    // switches, set in a label of its own before the switch. The administration
    // toggle has no icon and renders exactly as it did before.
    const iconName = this.getAttribute('icon');

    if (iconName) {
      const icon = document.createElement('i');

      icon.className = 'material-icons';
      icon.textContent = iconName;

      const label = document.createElement('span');

      label.className = 'rdio-toggle-label';
      label.append(icon);

      const row = document.createElement('span');

      row.className = 'rdio-toggle-row';
      row.append(label, button);
      this.append(row);
    } else {
      this.append(button);
    }

    const sync = () => {
      const on = this.getAttribute('checked') === 'true';
      button.setAttribute('aria-checked', String(on));
      button.classList.toggle('on', on);
    };

    button.addEventListener('click', () => {
      const on = this.getAttribute('checked') !== 'true';
      this.setAttribute('checked', String(on));
      sync();
      this.dispatchEvent(new CustomEvent('change', { detail: on }));
    });

    this.button = button;
    sync();
  }

  get checked() {
    return this.getAttribute('checked') === 'true';
  }

  set checked(v) {
    this.setAttribute('checked', String(v));
    this.button?.classList.toggle('on', !!v);
    this.button?.setAttribute('aria-checked', String(!!v));
  }
}

customElements.define('rdio-toggle', RdioToggle);

const paginatorControls = (material) =>
  material
    ? [
        ['first', 'first_page', 'First page'],
        ['prev', 'chevron_left', 'Previous page'],
        ['next', 'chevron_right', 'Next page'],
        ['last', 'last_page', 'Last page'],
      ]
    : [
        ['prev', '‹', 'Previous page'],
        ['next', '›', 'Next page'],
      ];

const paginatorMove = (paginator, name) =>
  ({
    first: () => paginator.setPage(0),
    prev: () => paginator.setPage(paginator.pageIndex - 1),
    next: () => paginator.setPage(paginator.pageIndex + 1),
    last: () => paginator.setPage(paginator.pageCount - 1),
  })[name];

class RdioPaginator extends HTMLElement {
  connectedCallback() {
    if (this.dom) return;

    // The listener's paginator is the four-control one that labels the range it
    // is showing ("1 of 10"). The administration paginator keeps the two
    // controls and the page counter it has always had.
    this.material = this.hasAttribute('material');

    const label = document.createElement('span');

    label.className = 'rdio-paginator-label';

    const dom = { label };
    const parts = [label];

    for (const [name, glyph, ariaLabel] of paginatorControls(this.material)) {
      const button = document.createElement('button');

      button.type = 'button';
      button.className = 'rdio-button-mini';
      button.setAttribute('aria-label', ariaLabel);

      if (this.material) {
        const icon = document.createElement('i');

        icon.className = 'material-icons';
        icon.textContent = glyph;
        button.append(icon);
      } else {
        button.textContent = glyph;
      }

      button.addEventListener('click', paginatorMove(this, name));
      dom[name] = button;
      parts.push(button);
    }

    if (this.material) {
      const range = document.createElement('div');

      range.className = 'rdio-paginator-range';
      range.append(...parts);
      this.append(range);
    } else {
      this.append(dom.prev, label, dom.next);
    }

    this.dom = dom;

    this.render();
  }

  get pageIndex() {
    return Number(this.getAttribute('page-index') ?? 0);
  }

  get pageSize() {
    return Number(this.getAttribute('page-size') ?? 10);
  }

  get length() {
    return Number(this.getAttribute('length') ?? 0);
  }

  get pageCount() {
    return Math.max(1, Math.ceil(this.length / this.pageSize));
  }

  setPage(index) {
    const max = this.pageCount - 1;
    index = Math.min(Math.max(0, index), max);

    if (index === this.pageIndex) return;

    this.setAttribute('page-index', String(index));
    this.render();
    this.dispatchEvent(new CustomEvent('page', { detail: { pageIndex: index } }));
  }

  setLength(length) {
    this.setAttribute('length', String(length));
    this.render();
  }

  firstPage() {
    if (this.pageIndex !== 0) {
      this.setAttribute('page-index', '0');
      this.render();
    }
  }

  rangeLabel() {
    if (this.length === 0) return '0 of 0';

    const from = this.pageIndex * this.pageSize + 1;
    const to = Math.min(from + this.pageSize - 1, this.length);

    // The legacy range label is written with an en dash, not a hyphen.
    return `${from} – ${to} of ${this.length}`;
  }

  render() {
    if (!this.dom) return;

    const { first, prev, label, next, last } = this.dom;
    const pages = this.pageCount;
    const atFirst = this.pageIndex === 0;
    const atLast = this.pageIndex >= pages - 1;

    label.textContent = this.material ? this.rangeLabel() : `Page ${this.pageIndex + 1} of ${pages}`;

    if (first) first.disabled = atFirst;
    if (last) last.disabled = atLast;

    prev.disabled = atFirst;
    next.disabled = atLast;
  }
}

customElements.define('rdio-paginator', RdioPaginator);
