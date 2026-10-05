// rdio-admin (T032): admin shell with sidebar navigation (replaces the legacy
// mat-accordion per decision-admin-section-api.md). One section page visible
// at a time; each section element loads and saves independently. Logout lives
// in the header. Unimplemented sections render a placeholder until their task
// registers the element.

import { getAdminContext } from '../../application/admin-context.js';
import './rdio-admin-login.js';
import './rdio-admin-todos.js';
import './rdio-admin-access.js';
import './rdio-admin-apikeys.js';
import './rdio-admin-dirwatch.js';
import './rdio-admin-downstreams.js';
import './rdio-admin-groups.js';
import './rdio-admin-options.js';
import './rdio-admin-logs.js';
import './rdio-admin-password.js';
import './rdio-admin-systems.js';
import './rdio-admin-tags.js';
import './rdio-admin-tools.js';

const SECTIONS = [
  { name: 'todos', label: 'Todos', icon: 'checklist' },
  { name: 'access', label: 'Access', icon: 'vpn_key' },
  { name: 'apikeys', label: 'API Keys', icon: 'key' },
  { name: 'dirwatch', label: 'Dirwatch', icon: 'folder', hiddenWhenDocker: true },
  { name: 'downstreams', label: 'Downstreams', icon: 'import_export' },
  { name: 'groups', label: 'Groups', icon: 'group' },
  { name: 'tags', label: 'Tags', icon: 'label' },
  { name: 'systems', label: 'Systems', icon: 'settings' },
  { name: 'options', label: 'Options', icon: 'tune' },
  { name: 'logs', label: 'Logs', icon: 'article' },
  { name: 'tools', label: 'Tools', icon: 'build' },
  { name: 'password', label: 'Password', icon: 'password' },
];

class RdioAdmin extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    getAdminContext().store.subscribe(['auth'], () => this.render());

    this.render();
  }

  render() {
    const { store, useCases } = getAdminContext();
    const { authenticated, docker } = store.getState().auth;

    if (!this.current) this.current = 'todos';

    if (!authenticated) {
      this.replaceChildren(document.createElement('rdio-admin-login'));

      return;
    }

    const header = document.createElement('header');
    header.className = 'rdio-admin-header';
    header.innerHTML = `<h1>Rdio Scanner Admin</h1>`;

    const logout = document.createElement('button');
    logout.type = 'button';
    logout.className = 'rdio-button logout';
    logout.innerHTML = `<i class="material-icons" aria-hidden="true">logout</i><span>Logout</span>`;
    logout.addEventListener('click', () => useCases.logout());
    header.append(logout);

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
    content.append(this.sectionElement(this.current));

    layout.append(nav, content);

    this.replaceChildren(header, layout);
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
