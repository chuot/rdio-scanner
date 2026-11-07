// rdio-admin: admin shell with sidebar navigation: one section page at a time
// section elements load and save independently; placeholders until registered

import { getAdminContext } from '../../application/admin-context.js';
import './openapi-box.js';
import './rdio-admin-openapi.js';
import './rdio-admin-login.js';
import './rdio-admin-access.js';
import './rdio-admin-apikeys.js';
import './rdio-admin-dashboard.js';
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

// keep SECTIONS in this order: the navigation is ordered by admin work flow, not
// labels.
const SECTIONS = [
  { name: 'dashboard', label: 'Dashboard', icon: 'speed' },
  { name: 'systems', label: 'Systems', icon: 'settings' },
  { name: 'sites', label: 'Sites', icon: 'place' },
  { name: 'talkgroups', label: 'Talkgroups', icon: 'group_work' },
  { name: 'units', label: 'Units', icon: 'directions_car' },
  { name: 'groups', label: 'Groups', icon: 'group' },
  { name: 'tags', label: 'Tags', icon: 'label' },
  { name: 'dirwatch', label: 'Dirwatch', icon: 'folder', hiddenWhenDocker: true },
  { name: 'downstreams', label: 'Downstreams', icon: 'import_export' },
  { name: 'apikeys', label: 'API Keys', icon: 'key' },
  { name: 'access', label: 'Access', icon: 'vpn_key' },
  { name: 'options', label: 'Options', icon: 'tune' },
  { name: 'logs', label: 'Logs', icon: 'article' },
  { name: 'tools', label: 'Tools', icon: 'build' },
  { name: 'openapi', label: 'OpenAPI', icon: 'api' },
];

// the navigation list above is the one place the section labels are written
const sectionLabel = (name) => SECTIONS.find((section) => section.name === name)?.label ?? name;

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

    // operators land on dashboard: it answers whether the deployment is working
    if (!this.current) this.current = 'dashboard';

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

    // a list element: screen readers can count the nav items; bare buttons announce none
    const list = document.createElement('ul');

    for (const section of SECTIONS) {
      if (section.hiddenWhenDocker && docker) continue;

      const item = document.createElement('li');

      const control = document.createElement('button');
      control.type = 'button';
      control.dataset.section = section.name;

      // only the current section gets aria-current="page": the others say nothing
      // aria-current="false" on every row makes a screen reader name thirteen routes
      if (section.name === this.current) control.setAttribute('aria-current', 'page');

      control.innerHTML = `<i class="material-icons" aria-hidden="true">${section.icon}</i><span>${section.label}</span>`;
      control.addEventListener('click', () => this.openSection(section.name));

      item.append(control);
      list.append(item);
    }

    nav.append(list);

    const content = document.createElement('section');
    content.className = 'rdio-admin-content';
    content.id = 'content';
    content.setAttribute('aria-labelledby', 'rdio-admin-section-heading');

    // the heading names the section, and the content region is labelled by it
    // the shell renders it: only it knows which section is on the page
    const heading = document.createElement('h2');
    heading.id = 'rdio-admin-section-heading';
    heading.textContent = sectionLabel(this.current);

    // the live region carries save and row-move answers; nothing static sits in it
    // the shell keeps one region across every repaint so a reply is not lost.
    if (!this.liveRegion) this.liveRegion = document.createElement('rdio-live-region');

    content.append(heading, this.liveRegion);

    if (banner) content.append(banner);

    content.append(this.sectionElement(this.current));

    layout.append(nav, content);

    this.replaceChildren(header, layout);
  }

  // todosBanner sits above the section card, edges aligned by the same width cap
  // it renders nothing when empty and is never dismissible.
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

      // a todo with a step opens the section owning the control that clears it
      if (!todo.step) {
        item.append(icon, label);
        banner.append(item);

        continue;
      }

      const step = document.createElement('button');
      step.type = 'button';
      step.className = 'todo-step';

      const go = document.createElement('i');
      go.className = 'material-icons arrow';
      go.setAttribute('aria-hidden', 'true');
      go.textContent = 'arrow_right';

      step.addEventListener('click', () => {
        getAdminContext().useCases.focusControl(todo.step.section, todo.step.control);

        this.openSection(todo.step.section);
      });

      step.append(icon, label, go);

      item.append(step);

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
