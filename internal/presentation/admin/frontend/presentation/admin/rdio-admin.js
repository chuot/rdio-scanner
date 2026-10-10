// rdio-admin (T032): admin shell with sidebar navigation (replaces the legacy
// mat-accordion per decision-admin-section-api.md). One section page visible
// at a time; each section element loads and saves independently. Logout lives
// in the header. Unimplemented sections render a placeholder until their task
// registers the element.

import { getAdminContext } from '../../application/admin-context.js';
import './openapi-box.js';
import './rdio-admin-openapi.js';
import './rdio-admin-login.js';
import './rdio-admin-access.js';
import './rdio-admin-apikeys.js';
import './rdio-admin-dirwatch.js';
import './rdio-admin-downstreams.js';
import './rdio-admin-groups.js';
import './rdio-admin-options.js';
import './rdio-admin-logs.js';
import './rdio-admin-sites.js';
import './rdio-admin-systems.js';
import './rdio-admin-talkgroups.js';
import './rdio-admin-tags.js';
import './rdio-admin-tools.js';
import './rdio-admin-units.js';

const SECTIONS = [
  { name: 'access', label: 'Access', icon: 'vpn_key' },
  { name: 'apikeys', label: 'API Keys', icon: 'key' },
  { name: 'dirwatch', label: 'Dirwatch', icon: 'folder', hiddenWhenDocker: true },
  { name: 'downstreams', label: 'Downstreams', icon: 'import_export' },
  { name: 'groups', label: 'Groups', icon: 'group' },
  { name: 'tags', label: 'Tags', icon: 'label' },
  { name: 'systems', label: 'Systems', icon: 'settings' },
  { name: 'sites', label: 'Sites', icon: 'place' },
  { name: 'talkgroups', label: 'Talkgroups', icon: 'group_work' },
  { name: 'units', label: 'Units', icon: 'directions_car' },
  { name: 'openapi', label: 'OpenAPI', icon: 'api' },
  { name: 'options', label: 'Options', icon: 'tune' },
  { name: 'logs', label: 'Logs', icon: 'article' },
  { name: 'tools', label: 'Tools', icon: 'build' },
];

class RdioAdmin extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    getAdminContext().store.subscribe(['auth', 'todos'], () => this.render());

    this.render();
  }

  render() {
    const { store, useCases } = getAdminContext();
    const { authenticated, docker } = store.getState().auth;

    if (!this.current) this.current = 'access';

    if (!authenticated) {
      this.replaceChildren(document.createElement('rdio-admin-login'));

      return;
    }

    const header = document.createElement('header');
    header.className = 'rdio-admin-header';

    const brand = document.createElement('div');
    brand.className = 'rdio-admin-brand';

    const logo = document.createElement('img');
    logo.className = 'rdio-admin-logo';
    logo.setAttribute('src', 'assets/icons/icon-192x192.png');
    logo.setAttribute('alt', 'Rdio Scanner');

    const title = document.createElement('h1');
    title.textContent = 'Rdio Scanner';

    brand.append(logo, title);
    header.append(brand);

    const logout = document.createElement('button');
    logout.type = 'button';
    logout.className = 'rdio-button logout';
    logout.innerHTML = `<i class="material-icons" aria-hidden="true">logout</i><span>Logout</span>`;
    logout.addEventListener('click', () => useCases.logout());
    header.append(logout);

    const banner = this.todosBanner(store.getState().todos ?? []);

    const layout = document.createElement('div');
    layout.className = 'rdio-admin-layout';

    const nav = document.createElement('nav');
    nav.className = 'rdio-admin-nav';
    nav.setAttribute('aria-label', 'Admin sections');

    for (const section of SECTIONS) {
      if (section.hiddenWhenDocker && docker) continue;

      const item = document.createElement('button');
      item.type = 'button';
      item.dataset.section = section.name;
      item.setAttribute('aria-current', section.name === this.current ? 'page' : 'false');
      item.innerHTML = `<i class="material-icons" aria-hidden="true">${section.icon}</i><span>${section.label}</span>`;
      item.addEventListener('click', () => this.openSection(section.name));

      nav.append(item);
    }

    const content = document.createElement('section');
    content.className = 'rdio-admin-content';

    if (banner) content.append(banner);

    content.append(this.sectionElement(this.current));

    layout.append(nav, content);

    this.replaceChildren(header, layout);
  }

  // todosBanner is the card the administrator sees first: it sits in the content
  // column above the section card, with the same width cap and card chrome, so its
  // edges line up with the section below. It renders nothing when the list is
  // empty, and it is never dismissible — a todo disappears when the administrator
  // fixes what caused it.
  todosBanner(todos) {
    if (todos.length === 0) return null;

    const banner = document.createElement('div');
    banner.className = 'rdio-admin-todos-banner';

    for (const todo of todos) {
      const item = document.createElement('div');
      item.className = `todo ${todo.level}`;

      const icon = document.createElement('i');
      icon.className = 'material-icons';
      icon.setAttribute('aria-hidden', 'true');
      icon.textContent = todo.level === 'warn' ? 'warning' : 'notifications';

      const label = document.createElement('span');
      label.textContent = todo.message;

      item.append(icon, label);
      banner.append(item);
    }

    return banner;
  }

  openSection(name) {
    if (this.current === name) return;

    this.current = name;

    this.render();
  }

  sectionElement(name) {
    const tag = `rdio-admin-${name}`;

    if (customElements.get(tag)) return document.createElement(tag);

    const placeholder = document.createElement('div');
    placeholder.className = 'rdio-admin-placeholder';
    placeholder.textContent = `${name} section is not available yet.`;

    return placeholder;
  }
}

customElements.define('rdio-admin', RdioAdmin);
