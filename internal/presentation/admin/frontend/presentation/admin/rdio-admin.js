// rdio-admin: admin shell with sidebar navigation (replaces the legacy
// mat-accordion). One section page visible
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

// The navigation is alphabetical by label: an operator looking for a section
// knows where to scan. Keep this list sorted.
const SECTIONS = [
  { name: 'access', label: 'Access', icon: 'vpn_key' },
  { name: 'apikeys', label: 'API Keys', icon: 'key' },
  { name: 'dirwatch', label: 'Dirwatch', icon: 'folder', hiddenWhenDocker: true },
  { name: 'downstreams', label: 'Downstreams', icon: 'import_export' },
  { name: 'groups', label: 'Groups', icon: 'group' },
  { name: 'logs', label: 'Logs', icon: 'article' },
  { name: 'openapi', label: 'OpenAPI', icon: 'api' },
  { name: 'options', label: 'Options', icon: 'tune' },
  { name: 'sites', label: 'Sites', icon: 'place' },
  { name: 'systems', label: 'Systems', icon: 'settings' },
  { name: 'tags', label: 'Tags', icon: 'label' },
  { name: 'talkgroups', label: 'Talkgroups', icon: 'group_work' },
  { name: 'tools', label: 'Tools', icon: 'build' },
  { name: 'units', label: 'Units', icon: 'directions_car' },
];

// The label the operator reads in the navigation, for the section the shell is putting on the page.
// The navigation list above is the one place the labels are written.
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

    // The sections are a list: a screen reader entering the navigation says how many items it
    // holds and reads them one by one, the way the operator scans the column with their eyes. Bare
    // buttons in the navigation announce a bar of controls with no count and no shape.
    const list = document.createElement('ul');

    for (const section of SECTIONS) {
      if (section.hiddenWhenDocker && docker) continue;

      const item = document.createElement('li');

      const control = document.createElement('button');
      control.type = 'button';
      control.dataset.section = section.name;

      // Only the section the operator is standing on is current. The others say nothing: an item
      // that announces aria-current="false" is a screen reader naming every section as part of a
      // route the operator is not on, thirteen times over.
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

    // The heading names the section the operator is on, and the content region takes its name from
    // it: a screen reader entering the region by landmark says the section, and the heading list the
    // operator jumps through carries every section the shell can put on the page. The shell renders
    // it because the shell is the one place that knows which section is on the page.
    const heading = document.createElement('h2');
    heading.id = 'rdio-admin-section-heading';
    heading.textContent = sectionLabel(this.current);

    // The polite region the page answers through: the save bar's result and a row's move land in
    // it, for the operator who is not looking at the rows when the page answers. It rides inside
    // the content region, below the heading naming it, and carries nothing static.
    content.append(heading, document.createElement('rdio-live-region'));

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

      // A todo that names the control clearing it is followed, not just read: the row
      // opens the section that owns the control and asks for its attention.
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
