// rdio-admin-todos (T032): admin todo list (legacy todos.component.ts).
// Todos are computed in domain/todos.js and pushed to the store by the admin
// use-cases on every config load / push.

import { getAdminContext } from '../../application/admin-context.js';

class RdioAdminTodos extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    const { store } = getAdminContext();

    this.unsubscribe = store.subscribe(['todos'], () => this.render());

    this.render();
  }

  disconnectedCallback() {
    this.unsubscribe?.();
  }

  render() {
    const todos = getAdminContext().store.getState().todos ?? [];

    if (!todos.length) {
      this.replaceChildren();

      return;
    }

    const card = document.createElement('div');
    card.className = 'rdio-admin-card todos';

    const title = document.createElement('h2');
    title.textContent = 'Todos';

    const list = document.createElement('ul');

    for (const todo of todos) {
      const item = document.createElement('li');
      item.className = `todo ${todo.level}`;
      item.innerHTML = `<i class="material-icons" aria-hidden="true">${todo.level === 'warn' ? 'warning' : 'notifications'}</i><span></span>`;
      item.querySelector('span').textContent = todo.message;

      list.append(item);
    }

    card.append(title, list);

    this.replaceChildren(card);
  }
}

customElements.define('rdio-admin-todos', RdioAdminTodos);
