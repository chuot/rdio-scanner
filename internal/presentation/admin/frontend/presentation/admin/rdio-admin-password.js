// rdio-admin-password (T045): admin password tool (legacy tools/password).
// Not a config section: no section bar, no store slice. Mirrors the legacy
// Angular form: readonly username field as a password-manager hint, required
// current/new/verify fields, new password minlength 8, group-level match
// error shown under the verify field, live validation, and the save() flow
// (disable → changePassword → snackbar → reset on success / re-enable on
// failure). Validation rules live in domain/password.js.

import { getAdminContext } from '../../application/admin-context.js';
import { validatePasswordChange } from '../../domain/password.js';
import { showSnackbar } from '../../../../common/frontend/presentation/ui/primitives.js';

class RdioAdminPassword extends HTMLElement {
  connectedCallback() {
    if (this.dataset.bound) return;
    this.dataset.bound = '1';

    this.innerHTML = `
      <div class="rdio-admin-card password">
        <p>Change the administrator password which gives access to the server administration dashboard.</p>

        <form novalidate>
          <rdio-form-field>
            <input type="text" name="username" value="admin" autocomplete="username" tabindex="-1" aria-label="Username (for password manager)" readonly />
          </rdio-form-field>

          <rdio-form-field>
            <label>Current password</label>
            <input type="password" name="currentPassword" required autocomplete="current-password" />
          </rdio-form-field>

          <rdio-form-field>
            <label>New password</label>
            <input type="password" name="newPassword" required minlength="8" autocomplete="new-password" />
          </rdio-form-field>

          <rdio-form-field>
            <label>Verify new password</label>
            <input type="password" name="verifyNewPassword" required autocomplete="new-password" />
          </rdio-form-field>

          <div class="row bottom">
            <button type="button" class="rdio-button reset" disabled>Reset</button>
            <button type="submit" class="rdio-button save" disabled>Save</button>
          </div>
        </form>
      </div>
    `;

    this.fields = {};

    for (const name of ['currentPassword', 'newPassword', 'verifyNewPassword']) {
      const input = this.querySelector(`input[name="${name}"]`);

      this.fields[name] = { input, field: input.closest('rdio-form-field') };

      input.addEventListener('input', () => {
        this.touched = true;
        this.validate();
      });
    }

    this.save = this.querySelector('button.save');
    this.resetButton = this.querySelector('button.reset');

    this.resetButton.addEventListener('click', () => this.clearForm());

    this.form = this.querySelector('form');

    this.form.addEventListener('submit', async (event) => {
      event.preventDefault();

      if (!this.validate()) return;

      this.setDisabled(true);

      const changed = await getAdminContext().useCases.changePassword(
        this.fields.currentPassword.input.value,
        this.fields.newPassword.input.value,
      );

      if (changed) {
        showSnackbar('Password changed successfully');
        this.clearForm();
      } else {
        showSnackbar('Unable to change password');
      }

      this.setDisabled(false);
      this.updateButtons();
    });
  }

  get values() {
    return {
      currentPassword: this.fields.currentPassword.input.value,
      newPassword: this.fields.newPassword.input.value,
      verifyNewPassword: this.fields.verifyNewPassword.input.value,
    };
  }

  validate() {
    const errors = validatePasswordChange(this.values);

    this.fields.currentPassword.field.setError(errors?.currentPassword ?? '');
    this.fields.newPassword.field.setError(errors?.newPassword ?? '');
    this.fields.verifyNewPassword.field.setError(errors?.verifyNewPassword ?? errors?.passwordMismatch ?? '');

    this.updateButtons(errors);

    return errors === null;
  }

  updateButtons(errors = validatePasswordChange(this.values)) {
    // legacy: Save disabled while pristine or invalid, Reset while untouched.
    this.save.disabled = !this.touched || errors !== null;
    this.resetButton.disabled = !this.touched;
  }

  clearForm() {
    for (const { input } of Object.values(this.fields)) input.value = '';
    for (const { field } of Object.values(this.fields)) field.setError('');

    this.touched = false;
    this.updateButtons();
  }

  setDisabled(disabled) {
    for (const control of this.form.elements) control.disabled = disabled;
  }
}

customElements.define('rdio-admin-password', RdioAdminPassword);
