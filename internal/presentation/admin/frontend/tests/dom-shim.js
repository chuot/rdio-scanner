// The smallest DOM the admin presentation components need to be exercised outside a
// browser: createElement, customElements, append/replaceChildren, textContent, dataset,
// attributes, and click listeners. innerHTML is stored as the string it is, never
// parsed, so a component that wants its test to see inside it builds real child nodes.
//
// The form field primitive is the custom element the shim builds and connects for real at every
// route a component uses to create it: createElement, append, replaceChildren, and the innerHTML
// parser. Its rule — show a message only once the operator has reached the field — is what the
// section proofs read, and a stub that records a string cannot carry it. Markup laid out through
// innerHTML carries every registered primitive the browser would upgrade, connected in document
// order, so a panel built that way is exercised as it is on screen. The other primitives stay as
// the plain nodes the tests connect by hand today.

const FORM_FIELD = 'rdio-form-field';

// The controls a browser hands the operator text in: their value is the empty string before
// anything is typed, and the components read them as text from the first render.
const CONTROL_TAGS = new Set(['input', 'textarea', 'select']);

// connectFormField runs the primitive's own connectedCallback the moment a component appends
// the field to a row, which is the moment a browser runs it: the field already carries its
// label and its control, and it is connected before the render writes messages into it.
function connectFormField(node) {
  if (node && node.tag === FORM_FIELD && typeof node.connectedCallback === 'function' && !node.dataset.bound) {
    node.connectedCallback();
  }
}

// connectCustom runs the connectedCallback of every custom element under a root, in the order a
// browser runs them: the element first, then what it builds into its children. Markup a component
// lays out through innerHTML is inserted as one tree, so every primitive in it is connected at that
// moment — the section's own setup then reads the controls its primitives build.
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

  // dataset and the data-* attributes are one thing in a browser: the builders write a row's key, a
  // field's key, and the mark that says a row is sortable through dataset, the markup the components
  // lay out carries the same facts as attributes, and the handlers read them back through dataset.
  // The shim keeps them as one thing, so a control marked through dataset and a control marked in
  // markup answer the same reading.
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

    // A browser links the nodes it inserts to the element they are inserted into: a section that
    // names a field with a caption in markup walks up from that caption to the row holding it, and
    // the links have to be there for the page to answer.
    for (const node of this.children) node.parent = this;

    // Inserting markup in a browser upgrades every custom element in it and runs their
    // connectedCallback in document order; a section that lays its panel out through innerHTML
    // then reads the controls its own primitives built.
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

    // `:scope >` is the child combinator the row lists use to name their own rows:
    // only the direct children of the element asked are candidates.
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

  // A control's value is the text in it, and a browser answers a control nobody has typed in
  // with the empty string, never with nothing. The controls the components read before the
  // operator reaches them — the log filters, the number fields — are read through this.
  get value() {
    if (this._value !== undefined) return this._value;

    return CONTROL_TAGS.has(this.tag) ? '' : undefined;
  }

  set value(value) {
    this._value = String(value);
  }

  // An element's id is the attribute the page writes it as: `el.id = 'content'` is how a browser
  // gives an element the name a skip link points at and a region is labelled by, and the tests read
  // that name back through getAttribute, the way the accessibility tree does.
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

  // The form field marks its control invalid with these two; the sections that clear a row's
  // warning icon take the icon off the page with remove().
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

  // classList is the class view the drag handlers use to mark the row being moved.
  // It reads and writes the same className the components set.
  get classList() {
    const node = this;

    const names = () => node.className.split(/\s+/).filter(Boolean);

    return {
      add: (...added) => (node.className = [...new Set([...names(), ...added])].join(' ')),
      remove: (...removed) => (node.className = names().filter((name) => !removed.includes(name)).join(' ')),
      contains: (name) => names().includes(name),
      // toggle(name, force) is the slide toggle's switch: the class is put on or taken off by
      // the value the caller passes, and the resulting state is what the caller reads back.
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

  // The dialog takes its own key handler off when it closes; a shim without this leaves the handler
  // on the element and the page.
  removeEventListener(type, fn) {
    this.listeners[type] = (this.listeners[type] ?? []).filter((f) => f !== fn);
  }

  // The form field primitive defines its own setError when it is connected, and that is the one
  // the section proofs read. This is the fallback for a field that was never connected: the shim
  // records the message so a test can still see the field was flagged.
  setError(error) {
    this.error = error;
  }

  // A click is answered with the event the browser hands the handler: the control the pointer was on
  // is the event's target, and the handler that answers a control inside a bigger control — a move
  // button inside a row's summary line — names the row through that target and takes the activation
  // the browser would otherwise give the surrounding control.
  click() {
    this.dispatchEvent({ type: 'click', target: this, bubbles: true });
  }

  // Forms refocus their input after a failed submit; the shim records the focus
  // so a test can see it happened.
  //
  // The focus lands where a browser puts it: on the element the keyboard is standing on, which the
  // page reads back through document.activeElement — the trap's ends, the tree that keeps focus
  // through a rebuild, and the panel that takes the keyboard in are all read through it.
  focus() {
    this.focused = true;
    document.activeElement = this;
  }

  // Forms are driven by dispatching their submit event, the way a browser does:
  // the listener receives the event object the test hands in. The event bubbles the way a
  // browser bubbles it: a section's handler sits on the control, the form field's own handler
  // sits on the field that owns the control, and a change typed into a control reaches both.
  //
  // An event the browser does not bubble stays on the element it was left on: the `toggle` event a
  // details element leaves when its summary is activated is one of them, so a handler on the list
  // under the rows never hears it.
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

    // A custom element instance is built through the class, whose super() has
    // no tag; the element name is the tag the component asked for.
    node.tag = tag;

    return node;
  },

  createTextNode: (text) => {
    const node = new ShimNode('#text');
    node._text = String(text);

    return node;
  },

  // document.body and document.querySelector are the two views the snackbar primitive uses to
  // place itself: a root node the components append to, and the lookup of what is in it.
  body: new ShimNode('body'),

  // The control the keyboard is standing on. A browser always has one: the page reads it back to
  // decide where its own focus rules have to put the keyboard next.
  activeElement: null,

  querySelector(selector) {
    return this.body.querySelectorAll(selector)[0] ?? null;
  },
};

globalThis.document.activeElement = globalThis.document.body;

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

// parseHTML is the smallest parser the innerHTML templates in the components need:
// tags, quoted attributes, text, and nesting. No entities, no comments, no
// self-closing rules beyond `/>`.
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

    // Every custom element in the markup is built through its own class, the way createElement
    // builds it, so a component that lays out a form through innerHTML gets the primitive's own
    // rule and not a node that only records what it was told.
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

  // A bare [attr] is the presence test the row lists use to name their rows. The
  // builders mark them through dataset, the browser's data-* view, so both spellings count.
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

// connectFields runs the form field primitive's connectedCallback on every field under a root,
// for the tests that drive a component whose markup arrived through innerHTML: a browser connects
// those fields when the tree is inserted, and the shim inserts them by parsing, not by appending.
export function connectFields(root) {
  const fields = findAll(root, (node) => node.tag === FORM_FIELD);

  for (const field of fields) {
    connectFormField(field);

    // The primitives a field carries connect after the field itself, the way a browser runs them:
    // the pick list builds the select the field answers with, and that select is the control a
    // label is attached to and a screen reader announces.
    for (const child of field.children ?? []) connectCustom(child);
  }

  return fields;
}

// fieldMessage is the text the operator sees under the control — the successor of mat-error.
// A field whose primitive is not connected has no message element, which is itself a reading:
// the message the section handed over was never written on the page.
export function fieldMessage(field) {
  const node = findAll(field, (child) => child.className?.split(/\s+/).includes('rdio-form-error'))[0];

  return node?.textContent ?? '';
}

// requiredMark is the asterisk a field's name carries: the mark the section declares on the element
// that names the field — the label of a form field, or the caption span a hand-built row names its
// field with. The glyph is the stylesheet's, so the reading is the mark and not the character: a name
// keeps its text. Both spellings count, the way the row lists read their own marks.
export function requiredMark(namingEl) {
  if (!namingEl) return false;

  return namingEl.dataset?.required === 'true' || namingEl.attributes?.['data-required'] === 'true';
}

// reachField is the operator editing a field and walking away from it: the change the browser fires
// when the value leaves the control, bubbling to the field that owns it. Walking across a field
// without editing it is not a reading — the rule Material's error matcher used.
export function reachField(control) {
  control.dispatchEvent({ type: 'change' });
}

// keystroke is the operator typing in the control and staying in it: the input event the browser
// fires as the value changes. The change reachField fires comes only when the operator leaves, so
// a reading taken here is the state of the page while the operator is in the field, with the value
// in the control and the section holding whatever was there before this keystroke.
export function keystroke(control, value) {
  control.value = value;
  control.dispatchEvent({ type: 'input' });
}
