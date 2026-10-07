// Shared UI primitives (T022): keyboard-operable custom elements replacing
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

  render(message) {
    this.hidden = !message;
    this.setAttribute('role', 'status');

    if (typeof message === 'string') this.textContent = message;
    else this.replaceChildren(message);
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

    const input = this.querySelector('input, select, textarea');

    const label = this.querySelector('label');
    if (label && input && !input.id) {
      input.id = `fld-${Math.random().toString(36).slice(2, 8)}`;
      label.htmlFor = input.id;
    }

    const error = document.createElement('div');
    error.className = 'rdio-form-error';
    error.setAttribute('role', 'alert');
    this.append(error);

    this.setError = (message) => {
      error.textContent = message ?? '';
      input?.toggleAttribute('aria-invalid', !!message);
    };
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
      this.dispatchEvent(new CustomEvent('change', { detail: select.value }));
    });

    this.append(select);
    this.select = select;
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
    this.append(button);
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

class RdioPaginator extends HTMLElement {
  connectedCallback() {
    if (this.dom) return;

    const prev = document.createElement('button');
    prev.type = 'button';
    prev.className = 'rdio-button-mini';
    prev.textContent = '‹';
    prev.setAttribute('aria-label', 'Previous page');

    const label = document.createElement('span');
    label.className = 'rdio-paginator-label';

    const next = document.createElement('button');
    next.type = 'button';
    next.className = 'rdio-button-mini';
    next.textContent = '›';
    next.setAttribute('aria-label', 'Next page');

    prev.addEventListener('click', () => this.setPage(this.pageIndex - 1));
    next.addEventListener('click', () => this.setPage(this.pageIndex + 1));

    this.dom = { prev, label, next };
    this.append(prev, label, next);

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

  setPage(index) {
    const max = Math.max(0, Math.ceil(this.length / this.pageSize) - 1);
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

  render() {
    if (!this.dom) return;

    const { prev, label, next } = this.dom;
    const pages = Math.max(1, Math.ceil(this.length / this.pageSize));

    label.textContent = `Page ${this.pageIndex + 1} of ${pages}`;
    prev.disabled = this.pageIndex === 0;
    next.disabled = this.pageIndex >= pages - 1;
  }
}

customElements.define('rdio-paginator', RdioPaginator);
