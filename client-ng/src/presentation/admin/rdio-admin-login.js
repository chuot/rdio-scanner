// rdio-admin-login (T031): admin login form (legacy login.component.ts).
// Password required; form disabled while submitting; failed attempts (wrong
// password or rate-limit 401) show "Invalid password" and re-enable the form.

import { getAdminContext } from '../../application/admin-context.js';

class RdioAdminLogin extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    this.innerHTML = `
      <form class="rdio-admin-login" autocomplete="off">
        <h1>Rdio Scanner Admin</h1>
        <rdio-form-field>
          <label>Password</label>
          <input name="password" type="password" required autofocus />
        </rdio-form-field>
        <button type="submit" class="rdio-button">Login</button>
      </form>
    `;

    this.form = this.querySelector('form');
    this.field = this.querySelector('rdio-form-field');
    this.input = this.querySelector('input[name="password"]');

    this.form.addEventListener('submit', async (event) => {
      event.preventDefault();

      const password = this.input.value;

      if (!password) return;

      this.setDisabled(true);
      this.field.setError('');

      const loggedIn = await getAdminContext().useCases.login(password);

      if (!loggedIn) {
        this.setDisabled(false);
        this.input.value = '';
        this.field.setError('Invalid password');
        this.input.focus();
      }
    });
  }

  setDisabled(disabled) {
    for (const control of this.form.elements) control.disabled = disabled;
  }
}

customElements.define('rdio-admin-login', RdioAdminLogin);
