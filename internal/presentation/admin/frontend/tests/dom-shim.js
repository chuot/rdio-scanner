// the smallest DOM the admin presentation components need outside a browser the
// form field primitive is built and connected for real on every creation route.

const FORM_FIELD = 'rdio-form-field';
const LIVE_REGION = 'rdio-live-region';

// text controls: their value is the empty string before anything is typed
const CONTROL_TAGS = new Set(['input', 'textarea', 'select']);

// connectFormField runs connectedCallback on append, as a browser does
// the shell places the live region empty; the shim connects it at insertion
function connectFormField(node) {
  if (node && (node.tag === FORM_FIELD || node.tag === LIVE_REGION) && typeof node.connectedCallback === 'function' && !node.dataset.bound) {
    node.connectedCallback();
  }
}

// connectCustom runs the connectedCallback of every custom element under a root,
// the element first, then its children, the order a browser runs them in
function connectCustom(node) {
  if (node.tag === '#text') return;

  if (node.tag.includes('-') && typeof node.connectedCallback === 'function') node.connectedCallback();

  for (const child of node.children ?? []) connectCustom(child);
}

class ShimNode {
  constructor(tag) {
    this.tag = tag;
    this.children = [];
    this.attributes = {};
    this.listeners = {};
    this.className = '';
    this._html = '';
    this._text = '';
  }

  // dataset and the data-* attributes are one thing here: a control marked
  // through dataset and a control marked in markup answer the same reading
  get dataset() {
    const node = this;

    const attribute = (key) => `data-${key.replace(/[A-Z]/g, (c) => `-${c.toLowerCase()}`)}`;

    return new Proxy({}, {
      get: (_target, key) => (typeof key === 'string' ? node.attributes[attribute(key)] : undefined),
      set: (_target, key, value) => {
        if (typeof key === 'string') node.attributes[attribute(key)] = String(value);

        return true;
      },
      has: (_target, key) => typeof key === 'string' && node.attributes[attribute(key)] !== undefined,
      deleteProperty: (_target, key) => {
        delete node.attributes[attribute(key)];

        return true;
      },
    });
  }

  // hidden attribute and hidden property are the same state, as in a browser
  get hidden() {
    return this.attributes.hidden !== undefined;
  }

  set hidden(value) {
    if (value) this.attributes.hidden = '';
    else delete this.attributes.hidden;
  }

  append(...nodes) {
    // The builders append bare strings as well as nodes; only the nodes have a parent.
    for (const node of nodes) {
      if (node && typeof node === 'object') {
        node.parent = this;
        connectFormField(node);
      }
    }

    this.children.push(...nodes);
  }

  // appendChild is the older spelling the primitives use to place a host on the page.
  appendChild(node) {
    this.append(node);

    return node;
  }

  replaceChildren(...nodes) {
    for (const node of this.children) if (node && typeof node === 'object') node.parent = null;
    for (const node of nodes) {
      if (node && typeof node === 'object') {
        node.parent = this;
        connectFormField(node);
      }
    }

    this.children = [...nodes];
  }

  set innerHTML(html) {
    this._html = html;
    this.children = parseHTML(html);

    // inserted nodes keep a parent link: a caption in markup walks up to its row
    for (const node of this.children) node.parent = this;

    // inserted markup upgrades every custom element in it and runs their
    // connectedCallback in document order, as a browser does
    for (const node of this.children) connectCustom(node);
  }

  get innerHTML() {
    return this._html;
  }

  querySelector(selector) {
    return this.querySelectorAll(selector)[0] ?? null;
  }

  querySelectorAll(selector) {
    const parts = selector.split(',').map((s) => s.trim());

    // `:scope >` is the child combinator the row lists use to name their own rows
    const scoped = parts.map((part) => part.match(/^:scope\s*>\s*(.+)$/));

    const found = [];

    const visit = (node) => {
      if (node && typeof node === 'object' && node.tag !== '#text') {
        parts.forEach((part, i) => {
          if (matches(node, scoped[i] ? scoped[i][1] : part)) {
            found.push(node);

            return;
          }
        });
      }

      for (const child of node?.children ?? []) visit(child);
    };

    if (scoped.every((part) => part)) {
      for (const child of this.children) visit(child);
    } else {
      visit(this);
    }

    return found;
  }

  set textContent(text) {
    this._text = String(text);
  }

  get textContent() {
    if (this._text) return this._text;

    return this.children.map((child) => child.textContent).join('');
  }

  setAttribute(name, value) {
    this.attributes[name] = String(value);
  }

  // a control nobody has typed in answers with the empty string, never with nothing
  get value() {
    if (this._value !== undefined) return this._value;

    return CONTROL_TAGS.has(this.tag) ? '' : undefined;
  }

  set value(value) {
    this._value = String(value);
  }

  // el.id is the id attribute: skip links read it back through getAttribute
  get id() {
    return this.attributes.id ?? '';
  }

  set id(value) {
    this.attributes.id = String(value);
  }

  getAttribute(name) {
    return this.attributes[name] ?? null;
  }

  // hasAttribute is the presence test the primitives use to pick the shape they render.
  hasAttribute(name) {
    return this.attributes[name] !== undefined;
  }

  // these two mark a control invalid and clear a row's warning icon
  removeAttribute(name) {
    delete this.attributes[name];
  }

  toggleAttribute(name, on) {
    if (on) this.setAttribute(name, '');
    else this.removeAttribute(name);

    return on;
  }

  remove() {
    const parent = this.parent;

    if (parent) parent.children = parent.children.filter((node) => node !== this);

    this.parent = null;
  }

  // isConnected: a node is connected while its parents reach the document;
  // a removed subtree reports not connected, as does a never-placed node
  // the shim tracks parents but runs no disconnected callback on removal.
  get isConnected() {
    let node = this;

    while (node.parent) node = node.parent;

    return node === globalThis.document?.body;
  }

  // classList reads and writes the same className the drag handlers set
  get classList() {
    const node = this;

    const names = () => node.className.split(/\s+/).filter(Boolean);

    return {
      add: (...added) => (node.className = [...new Set([...names(), ...added])].join(' ')),
      remove: (...removed) => (node.className = names().filter((name) => !removed.includes(name)).join(' ')),
      contains: (name) => names().includes(name),
      // toggle(name, force) puts the class on or off by the value passed
      toggle: (name, force) => {
        const on = force === undefined ? !names().includes(name) : !!force;

        node.className = on ? [...new Set([...names(), name])].join(' ') : names().filter((n) => n !== name).join(' ');

        return on;
      },
    };
  }

  // closest walks the node and its parents, the way a browser does: the drag handlers
  // name the row a control inside the row belongs to.
  closest(selector) {
    for (let node = this; node; node = node.parent) {
      if (node.tag !== '#text' && matches(node, selector)) return node;
    }

    return null;
  }

  addEventListener(type, fn) {
    (this.listeners[type] ??= []).push(fn);
  }

  // the dialog removes its key handler on close; without this the handler stays
  removeEventListener(type, fn) {
    this.listeners[type] = (this.listeners[type] ?? []).filter((f) => f !== fn);
  }

  // fallback setError for a never-connected field: the shim records the message
  // tests still see the flagged field; the primitive defines its own when connected
  setError(error) {
    this.error = error;
  }

  // the click event carries target, the control the pointer was on:
  // a handler on a control inside a bigger one names the row through the target
  // preventDefault and stopPropagation take the activation from the outer control
  click() {
    this.dispatchEvent({
      type: 'click',
      target: this,
      bubbles: true,
      preventDefault: () => {},
      stopPropagation: () => {},
    });
  }

  // focus sets the element the keyboard stands on, read as document.activeElement
  // a form refocuses its input after a failed submit, and the shim records that
  focus() {
    this.focused = true;
    document.activeElement = this;
  }

  // events bubble up to the owning field, as in a browser an event the
  // browser does not bubble stays put, like details' toggle.
  dispatchEvent(event) {
    const bubbles = event.bubbles !== false;

    for (let node = this; node; node = node.parent) {
      for (const fn of node.listeners?.[event.type] ?? []) fn(event);

      if (!bubbles) break;
    }
  }

  // form.elements is the control list the login form disables while submitting:
  // the shim answers with the form's descendant controls, in document order.
  get elements() {
    if (this.tag !== 'form') return undefined;

    const controls = [];

    const walk = (node) => {
      for (const child of node.children ?? []) {
        if (['input', 'button', 'select', 'textarea'].includes(child.tag)) controls.push(child);
        walk(child);
      }
    };

    walk(this);

    return controls;
  }
}

globalThis.HTMLElement = ShimNode;

globalThis.document = {
  createElement: (tag) => {
    const ctor = globalThis.customElements.registry.get(tag);

    const node = ctor ? new ctor() : new ShimNode(tag);

    // the class's super() has no tag: createElement stamps the custom element's tag
    node.tag = tag;

    return node;
  },

  createTextNode: (text) => {
    const node = new ShimNode('#text');
    node._text = String(text);

    return node;
  },

  // document.body and querySelector are the snackbar primitive's placement views
  body: new ShimNode('body'),

  // activeElement is the control the keyboard stands on; a browser always has one
  activeElement: null,

  querySelector(selector) {
    return this.body.querySelectorAll(selector)[0] ?? null;
  },
};

globalThis.document.activeElement = globalThis.document.body;

// the shim carries the two page-visibility reads: visibilityState
// and the visibilitychange event that says it changed
globalThis.document.visibilityState = 'visible';

globalThis.document.listeners = new Map();

globalThis.document.addEventListener = function (type, handler) {
  const handlers = this.listeners.get(type) ?? new Set();

  handlers.add(handler);

  this.listeners.set(type, handlers);
};

globalThis.document.removeEventListener = function (type, handler) {
  this.listeners.get(type)?.delete(handler);
};

globalThis.document.dispatchEvent = function (event) {
  for (const handler of this.listeners.get(event.type) ?? []) handler(event);
};

// setPageVisibility is the browser putting the page somewhere else: the state changes and every page that
// is listening is told, in the order the page registered.
export function setPageVisibility(state) {
  globalThis.document.visibilityState = state;

  globalThis.document.dispatchEvent({ type: 'visibilitychange' });
}

globalThis.customElements = {
  registry: new Map(),

  define(name, ctor) {
    this.registry.set(name, ctor);
  },

  get(name) {
    return this.registry.get(name);
  },
};

function fakeStorage() {
  const store = new Map();

  return {
    getItem: (key) => (store.has(key) ? store.get(key) : null),
    setItem: (key, value) => store.set(key, String(value)),
    removeItem: (key) => store.delete(key),
    clear: () => store.clear(),
  };
}

globalThis.window = {
  location: new URL('http://localhost/admin/'),
  sessionStorage: fakeStorage(),
};

// The primitives re-dispatch a CustomEvent carrying the value the control picked, and a test that
// drives a control through its own event needs the constructor the browser hands them.
globalThis.CustomEvent = class CustomEvent {
  constructor(type, init = {}) {
    this.type = type;
    this.detail = init.detail;
    this.bubbles = !!init.bubbles;
  }
};

// walk visits every node under a root, so a test can find a node by a predicate
// without depending on the order the shell builds its children in.
export function walk(root, visit) {
  visit(root);

  for (const child of root.children ?? []) walk(child, visit);
}

// parseHTML covers what the innerHTML templates need: tags, quoted attributes,
// text, nesting; no entities, no comments, no self-closing rules beyond `/>`
function parseHTML(html) {
  const roots = [];
  const stack = [];

  for (const token of html.match(/<[^>]+>|[^<]+/g) ?? []) {
    if (!token.startsWith('<')) {
      const text = token.trim();

      if (!text) continue;

      const node = new ShimNode('#text');
      node._text = text;

      if (stack.length) node.parent = stack[stack.length - 1];

      (stack.length ? stack[stack.length - 1].children : roots).push(node);

      continue;
    }

    if (token.startsWith('</')) {
      stack.pop();

      continue;
    }

    // custom elements in markup use their own class, as createElement builds them
    // so innerHTML forms get the primitive's rule, not a node that records markup
    const tag = token.match(/^<([a-zA-Z][\w-]*)/)[1];
    const ctor = globalThis.customElements.registry.get(tag);

    const node = ctor ? new ctor() : new ShimNode(tag);

    node.tag = tag;

    for (const attr of token.matchAll(/([a-zA-Z-]+)="([^"]*)"/g)) {
      node.attributes[attr[1]] = attr[2];

      if (attr[1] === 'class') node.className = attr[2];
    }

    (stack.length ? stack[stack.length - 1].children : roots).push(node);

    if (stack.length) node.parent = stack[stack.length - 1];

    if (!token.endsWith('/>')) stack.push(node);
  }

  return roots;
}

function matches(node, selector) {
  if (selector.startsWith('.')) {
    return node.className.split(/\s+/).includes(selector.slice(1));
  }

  const tag = selector.match(/^([a-zA-Z][\w-]*)/)?.[0];

  if (tag && node.tag !== tag) return false;

  const attr = selector.match(/\[([a-zA-Z-]+)="([^"]*)"\]/);

  if (attr && node.attributes[attr[1]] !== attr[2]) return false;

  // a bare [attr] is a presence test: the builders mark rows through dataset,
  // the browser's data-* view, so both spellings count
  const has = selector.match(/\[([a-zA-Z-]+)\]/);

  if (has) {
    const camel = has[1].replace(/^data-/, '').replace(/-(.)/g, (m, c) => c.toUpperCase());

    return node.attributes[has[1]] !== undefined || node.dataset[camel] !== undefined;
  }

  return true;
}

export function findAll(root, predicate) {
  const found = [];

  walk(root, (node) => {
    if (predicate(node)) found.push(node);
  });

  return found;
}

export function texts(nodes) {
  return nodes.map((node) => node.textContent);
}

// connectFields connects the form field primitives built through innerHTML:
// a browser connects them at insertion, the shim inserts them by parsing
export function connectFields(root) {
  const fields = findAll(root, (node) => node.tag === FORM_FIELD);

  for (const field of fields) {
    connectFormField(field);

    // a field's own primitives connect after the field, as a browser runs them:
    // the pick list builds the select a label attaches to and is announced with
    for (const child of field.children ?? []) connectCustom(child);
  }

  return fields;
}

// fieldMessage is the text under the control; a disconnected field has no message
// element, so '' says the section's message was never written on the page
export function fieldMessage(field) {
  const node = findAll(field, (child) => child.className?.split(/\s+/).includes('rdio-form-error'))[0];

  return node?.textContent ?? '';
}

// requiredMark is the asterisk on the element naming the field: a form field's
// label, or the caption span a hand-built row names its field with
// the glyph is the stylesheet's, so the name keeps its text; both spellings count
export function requiredMark(namingEl) {
  if (!namingEl) return false;

  return namingEl.dataset?.required === 'true' || namingEl.attributes?.['data-required'] === 'true';
}

// reachField fires the browser's change when the edited value leaves the control
// walking across a field without editing it is not a reading
export function reachField(control) {
  control.dispatchEvent({ type: 'change' });
}

// keystroke fires the input event the browser sends as the value changes
// reachField's change comes only on leaving: a reading here is the page mid-edit,
// value in the control, the section holding what was there before the keystroke
export function keystroke(control, value) {
  control.value = value;
  control.dispatchEvent({ type: 'input' });
}
