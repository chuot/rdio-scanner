// The smallest DOM the listener presentation components need to be exercised outside a browser:
// createElement, customElements, append/replaceChildren, textContent, attributes, and class names.
// innerHTML is stored as the string it is and parsed into real child nodes, so a component that builds
// its markup in innerHTML can be read from the inside by a test.
//
// The globals the application's composition point touches on the way in are here too, inert: a
// WebSocket that never opens, an audio context that never produces sound, a localStorage of its own.
// A component test is about what the component shows, and nothing here is meant to behave like a
// browser beyond the nodes a test reads.

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

  append(...nodes) {
    this.children.push(...nodes);
  }

  replaceChildren(...nodes) {
    this.children = [...nodes];
  }

  set innerHTML(html) {
    this._html = html;
    this.children = parseHTML(html);
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

  getAttribute(name) {
    return this.attributes[name] ?? null;
  }

  addEventListener(type, fn) {
    (this.listeners[type] ??= []).push(fn);
  }

  removeEventListener(type, fn) {
    this.listeners[type] = (this.listeners[type] ?? []).filter((f) => f !== fn);
  }

  dispatchEvent(event) {
    for (const fn of this.listeners[event.type] ?? []) fn(event);
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
};

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

      (stack.length ? stack[stack.length - 1].children : roots).push(node);

      continue;
    }

    if (token.startsWith('</')) {
      stack.pop();

      continue;
    }

    const node = new ShimNode(token.match(/^<([a-zA-Z][\w-]*)/)[1]);

    for (const attr of token.matchAll(/([a-zA-Z-]+)="([^"]*)"/g)) {
      node.attributes[attr[1]] = attr[2];

      if (attr[1] === 'class') node.className = attr[2];
    }

    (stack.length ? stack[stack.length - 1].children : roots).push(node);

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
