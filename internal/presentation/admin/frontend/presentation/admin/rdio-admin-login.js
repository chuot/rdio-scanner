// rdio-admin-login: admin login form (legacy login.component.ts).
// Password required; form disabled while submitting; a wrong password shows
// "Invalid password", and a server that cannot be reached is named as such —
// the form re-enables for a retry either way.

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
          <input name="password" type="password" autocomplete="current-password" required autofocus />
        </rdio-form-field>
        <button type="submit" class="rdio-button">Login</button>
      </form>
    `;

    this.form = this.querySelector('form');
    this.field = this.querySelector('rdio-form-field');
    this.input = this.querySelector('input[name="password"]');

    this.form.addEventListener('submit', async (event) => {
      event.preventDefault();

      // A submit reaches the field it submits: the operator may have pressed Enter without ever
      // leaving the field, and the failure the server answers has to be said out loud.
      this.field.markReached();

      const password = this.input.value;

      if (!password) return;

      this.setDisabled(true);
      this.field.setError('');

      const result = await getAdminContext().useCases.login(password);

      if (result !== 'ok') {
        this.setDisabled(false);
        this.input.value = '';
        this.field.setError(result === 'unreachable'
          ? 'The server cannot be reached. Check that it is running.'
          : 'Invalid password');
        this.input.focus();
      }
    });
  }

  setDisabled(disabled) {
    for (const control of this.form.elements) control.disabled = disabled;
  }
}

customElements.define('rdio-admin-login', RdioAdminLogin);
