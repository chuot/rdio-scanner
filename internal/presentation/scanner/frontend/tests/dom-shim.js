// The smallest DOM the listener presentation components need to be exercised outside a browser:
// createElement, customElements, append/replaceChildren, textContent, attributes, and class names.
// innerHTML is stored as the string it is and parsed into real child nodes, so a component that builds
// its markup in innerHTML can be read from the inside by a test.
//
// The globals the application's composition point touches on the way in are here too, inert: a
// WebSocket that never opens, an audio context that never produces sound, a localStorage of its own.
// A component test is about what the component shows, and nothing here is meant to behave like a
// browser beyond the nodes a test reads.

const FORM_FIELD = 'rdio-form-field';

// connectFormField runs the primitive's own connectedCallback the moment a component appends the
// field, which is the moment a browser runs it: the field already carries its control, and it is
// connected before anything writes a message into it.
function connectFormField(node) {
  if (node && node.tag === FORM_FIELD && typeof node.connectedCallback === 'function' && !node.dataset.bound) {
    node.connectedCallback();
  }
}

// connectCustom runs the connectedCallback of every custom element under a root, in the order a
// browser runs them: the element first, then what it builds into its children. Markup a component
// lays out through innerHTML is inserted as one tree, so every element in it is connected at that
// moment — the shell that lays out its panels then reads the controls its own elements build inside
// them.
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

  // hidden is one fact in a browser: the attribute the markup carries and the property the page
  // sets are the same state. A panel hidden in markup and opened by property, and a control the page
  // takes off the screen, have to answer the same reading.
  get hidden() {
    return this.attributes.hidden !== undefined;
  }

  set hidden(value) {
    if (value) this.attributes.hidden = '';
    else delete this.attributes.hidden;
  }

  append(...nodes) {
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

    // A browser connects every custom element the moment the tree it parsed is inserted, in document
    // order: the element first, then what it builds into its children. The roots of a parsed tree
    // hang under the node that was given the markup, and are connected with it.
    for (const node of this.children) if (node && typeof node === 'object') node.parent = this;

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

    const found = [];

    walk(this, (node) => {
      if (node.tag === '#text') return;

      for (const part of parts) {
        if (matches(node, part)) {
          found.push(node);

          break;
        }
      }
    });

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

  // The form field marks its control invalid with these two, and takes the mark off again.
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

  // classList is the class view the components toggle to show and hide a panel, light a flag, and
  // mark the control being moved. It reads and writes the same className the components set.
  get classList() {
    const node = this;

    const names = () => node.className.split(/\s+/).filter(Boolean);

    return {
      add: (...added) => (node.className = [...new Set([...names(), ...added])].join(' ')),
      remove: (...removed) => (node.className = names().filter((name) => !removed.includes(name)).join(' ')),
      contains: (name) => names().includes(name),
      toggle: (name, force) => {
        const on = force === undefined ? !names().includes(name) : !!force;

        return on ? this.classList.add(name) : this.classList.remove(name);
      },
    };
  }

  // closest walks the node and its parents, the way a browser does: the control bar names the button
  // a click landed on, and the row a control inside a row belongs to.
  closest(selector) {
    for (let node = this; node; node = node.parent) {
      if (node.tag !== '#text' && matches(node, selector)) return node;
    }

    return null;
  }

  // A scroll container answers the page's request to move to a place inside it. The shim has no
  // viewport: the position is recorded, which is all a reading can read.
  scrollTo(x, y) {
    this.scrollLeft = x;
    this.scrollTop = y;
  }

  // The layout question the drawers ask to force a reflow before the open position is applied. The
  // shim has no layout: the answer is the empty box a node that is not laid out has.
  getBoundingClientRect() {
    return { bottom: 0, height: 0, left: 0, right: 0, top: 0, width: 0, x: 0, y: 0 };
  }

  // A form's elements are the controls it carries, named by their `name`, the way a browser hands
  // them to the page: the search panel reads the filters the operator set through them.
  get elements() {
    if (this.tag !== 'form') return undefined;

    const named = {};

    for (const control of findAll(this, (node) => ['input', 'select', 'textarea', 'button'].includes(node.tag))) {
      const name = control.attributes.name ?? control.name;

      if (name) named[name] = control;
    }

    return named;
  }

  addEventListener(type, fn) {
    (this.listeners[type] ??= []).push(fn);
  }

  removeEventListener(type, fn) {
    this.listeners[type] = (this.listeners[type] ?? []).filter((f) => f !== fn);
  }

  // A click is answered with the event the browser hands the handler: the control the pointer was on
  // is the event's target, and a handler sitting on a container — the shell's panel close buttons,
  // the controls that open a panel — names the control through that target.
  click() {
    this.dispatchEvent({ type: 'click', target: this, bubbles: true });
  }

  // The auth panel refocuses the unlock code field after a refused code; the shim records the
  // focus so a test can see it happened.
  //
  // The focus lands where a browser puts it: on the control the keyboard is standing on, which the
  // page reads back through document.activeElement — the panel that takes the keyboard in, and the
  // control that gets it back when the panel closes, are both read through it.
  focus() {
    this.focused = true;
    document.activeElement = this;
  }

  blur() {
    this.focused = false;

    if (document.activeElement === this) document.activeElement = document.body;
  }

  // Events bubble the way a browser bubbles them: the panel's handler sits on the form, the form
  // field's own handler sits on the field that owns the control, and a change typed into a control
  // reaches both.
  dispatchEvent(event) {
    for (let node = this; node; node = node.parent) {
      for (const fn of node.listeners?.[event.type] ?? []) fn(event);
    }
  }
}

globalThis.HTMLElement = ShimNode;

globalThis.document = {
  body: new ShimNode('body'),

  createElement: (tag) => {
    const ctor = globalThis.customElements.registry.get(tag);

    const node = ctor ? new ctor() : new ShimNode(tag);

    // A custom element instance is built through the class, whose super() has no tag; the element
    // name is the tag the component asked for.
    node.tag = tag;

    return node;
  },

  createTextNode: (text) => {
    const node = new ShimNode('#text');
    node._text = String(text);

    return node;
  },

  // document.body and document.querySelector are the two views the snackbar primitive uses to place
  // itself: a root node the components append to, and the lookup of what is in it.
  querySelector(selector) {
    return this.body.querySelectorAll(selector)[0] ?? null;
  },

  // The control the keyboard is standing on. A browser always has one: the page reads it back to
  // decide where its own focus rules have to put the keyboard next.
  activeElement: null,

  // The document is an event target of its own: the fullscreen state the browser moves the page into
  // is announced on the document, and the page listens there.
  listeners: {},

  addEventListener(type, fn) {
    (this.listeners ??= {})[type] = [...(this.listeners?.[type] ?? []), fn];
  },

  removeEventListener(type, fn) {
    this.listeners[type] = (this.listeners[type] ?? []).filter((f) => f !== fn);
  },

  dispatchEvent(event) {
    for (const fn of this.listeners?.[event.type] ?? []) fn(event);

    return true;
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

// The listener's own composition point reads its instance id and its websocket address out of the
// location, the way a browser's location object answers them as strings.
globalThis.window = {
  href: 'http://localhost/',
  localStorage: fakeStorage(),
  location: {
    href: 'http://localhost/',
    pathname: '/',
    replace: (href) => {
      globalThis.window.location.href = href;
    },
  },

  // The listener shell registers the guard a browser asks the page before it leaves, and takes it
  // off again when the shell is taken off the page. The shim keeps the listeners it is given, so a
  // test can render the shell and disconnect it the way the page does, and read what it registered.
  addEventListener(type, fn) {
    (this.listeners ??= {})[type] = [...(this.listeners?.[type] ?? []), fn];
  },

  removeEventListener(type, fn) {
    this.listeners = { ...this.listeners, [type]: (this.listeners?.[type] ?? []).filter((f) => f !== fn) };
  },
};

// The socket adapter opens one of these the moment the context is built. Nothing arrives, and
// nothing is sent: a component test drives the component, not the server.
globalThis.WebSocket = class {
  constructor(url) {
    this.url = url;
    this.sent = [];
  }

  send(data) {
    this.sent.push(data);
  }

  close() {}
};

// The audio engine builds its contexts on the first gesture. A component test presses nothing, so
// this is only here so a test that does press something cannot reach a real audio device.
globalThis.AudioContext = class {
  constructor() {
    this.state = 'suspended';
    this.destination = {};
  }

  resume() {
    return Promise.resolve();
  }
};

// The primitives re-dispatch a CustomEvent carrying the value a control picked, and the controls
// announce their presses as CustomEvents; a test that drives a component needs the constructor the
// browser hands them.
globalThis.CustomEvent = class CustomEvent {
  constructor(type, init = {}) {
    this.type = type;
    this.detail = init.detail;
    this.bubbles = !!init.bubbles;
  }
};

// walk visits every node under a root, so a test can find a node by a predicate without depending on
// the order the component builds its children in.
export function walk(root, visit) {
  visit(root);

  for (const child of root.children ?? []) walk(child, visit);
}

// parseHTML is the smallest parser the innerHTML templates in the components need: tags, quoted
// attributes, text, and nesting. No entities, no comments, no self-closing rules beyond `/>`.
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

    // Every registered custom element is built through its own class, the way createElement builds
    // it: a panel that lays its content out through innerHTML gets the primitives' own behaviour, as
    // a browser upgrades what it inserts. The form field is the case the primitives write into from
    // their own connectedCallback, and it is built the same way as the rest.
    const tag = token.match(/^<([a-zA-Z][\w-]*)/)[1];
    const ctor = globalThis.customElements.registry.get(tag);

    const node = ctor ? new ctor() : new ShimNode(tag);

    node.tag = tag;

    for (const attr of token.matchAll(/([a-zA-Z-]+)="([^"]*)"/g)) {
      node.attributes[attr[1]] = attr[2];

      if (attr[1] === 'class') node.className = attr[2];
    }

    if (stack.length) node.parent = stack[stack.length - 1];

    (stack.length ? stack[stack.length - 1].children : roots).push(node);

    if (!token.endsWith('/>')) stack.push(node);
  }

  return roots;
}

function matches(node, selector) {
  // A descendant selector — `.auth form`, `.history tbody` — names the control by the panel it sits
  // in: the node answers the last part, one of its ancestors answers the part before it.
  const parts = selector.trim().split(/\s+/);

  if (parts.length > 1) {
    if (node.tag === '#text' || !matches(node, parts[parts.length - 1])) return false;

    let ancestor = node.parent;

    for (let i = parts.length - 2; i >= 0; i--) {
      let found = null;

      for (let walk = ancestor; walk; walk = walk.parent) {
        if (walk.tag !== '#text' && matches(walk, parts[i])) {
          found = walk.parent;
          break;
        }
      }

      if (!found) return false;

      ancestor = found;
    }

    return true;
  }

  // A class selector can name more than one class at once — `.panel.search` is how the listener
  // shell names its own search drawer — so every class the selector lists has to be on the node.
  if (selector.startsWith('.')) {
    const classes = selector.match(/\.[A-Za-z0-9_-]+/g) ?? [];

    return classes.every((name) => node.className.split(/\s+/).includes(name.slice(1)));
  }

  // `#id` names the control by its id, the way the panels pick their controls out of their markup.
  if (selector.startsWith('#')) {
    return node.attributes.id === selector.slice(1);
  }

  const tag = selector.match(/^([a-zA-Z][\w-]*)/)?.[0];

  if (tag && node.tag !== tag) return false;

  const attr = selector.match(/\[([a-zA-Z-]+)="([^"]*)"\]/);

  if (attr && node.attributes[attr[1]] !== attr[2]) return false;

  // A bare [attr] is the presence test the page uses to name the controls that carry a mark: the
  // builders write it through dataset, the browser's data-* view, so both spellings count. Without
  // it, a selector naming an attribute a control carries matches every node.
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

// connectFields runs the form field primitive's connectedCallback on every field under a root, for
// the tests that drive a panel whose markup arrived through innerHTML: a browser connects those
// fields when the tree is inserted, and the shim inserts them by parsing, not by appending.
export function connectFields(root) {
  const fields = findAll(root, (node) => node.tag === FORM_FIELD);

  for (const field of fields) connectFormField(field);

  return fields;
}

// fieldMessage is the text the listener sees under the control — the successor of mat-error. A
// field whose primitive is not connected has no message element, which is itself a reading: the
// message was never written on the page.
export function fieldMessage(field) {
  const node = findAll(field, (child) => child.className?.split(/\s+/).includes('rdio-form-error'))[0];

  return node?.textContent ?? '';
}
