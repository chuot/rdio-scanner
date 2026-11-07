// the password panel is a Tools details panel, not a section; no username field;
// save: disable, change, snackbar, reset on success, re-enable on failure.

import { getAdminContext } from '../../application/admin-context.js';
import { validatePasswordChange } from '../../domain/password.js';
import { showSnackbar } from '../../../../common/frontend/presentation/ui/primitives.js';
import { markRequired } from './row-builders.js';

const FIELDS = [
  ['currentPassword', 'Current password', 'current-password'],
  ['newPassword', 'New password', 'new-password'],
  ['verifyNewPassword', 'Verify new password', 'new-password'],
];

export function passwordPanel() {
  const details = document.createElement('details');
  details.className = 'rdio-admin-row';

  const summary = document.createElement('summary');

  const icon = document.createElement('i');
  icon.className = 'material-icons';
  icon.setAttribute('aria-hidden', 'true');
  icon.textContent = 'lock';

  const title = document.createElement('span');
  title.className = 'row-title';
  title.textContent = 'Change password';

  summary.append(icon, title);
  details.append(summary);

  const help = document.createElement('p');
  help.textContent = 'Change the administrator password which gives access to the server administration dashboard.';

  const form = document.createElement('form');
  form.setAttribute('novalidate', '');

  const fields = {};
  const controls = [];

  for (const [name, label, autocomplete] of FIELDS) {
    const field = document.createElement('rdio-form-field');

    const labelEl = document.createElement('label');
    labelEl.textContent = label;

    const input = document.createElement('input');
    input.setAttribute('type', 'password');
    input.setAttribute('name', name);
    input.setAttribute('required', '');
    input.setAttribute('autocomplete', autocomplete);

    if (name === 'newPassword') input.setAttribute('minlength', '8');

    field.append(labelEl, input);

    // the server takes only the two passwords: every box here is the webapp's own, and
    // each field shows the visible marker and announced pair on its host.
    markRequired(field, labelEl);

    form.append(field);

    fields[name] = { input, field };
    controls.push(input);
  }

  const bottom = document.createElement('div');
  bottom.className = 'row bottom';

  const reset = document.createElement('button');
  reset.type = 'button';
  reset.className = 'rdio-button reset';
  reset.textContent = 'Reset';
  reset.disabled = true;

  const save = document.createElement('button');
  save.type = 'submit';
  save.className = 'rdio-button save';
  save.textContent = 'Save';
  save.disabled = true;

  bottom.append(reset, save);
  form.append(bottom);

  const touched = { value: false };

  const updateButtons = (errors = validatePasswordChange(values())) => {
    // Save stays disabled while pristine or invalid.
    save.disabled = !touched.value || errors !== null;
    reset.disabled = !touched.value;
  };

  const values = () => ({
    currentPassword: fields.currentPassword.input.value ?? '',
    newPassword: fields.newPassword.input.value ?? '',
    verifyNewPassword: fields.verifyNewPassword.input.value ?? '',
  });

  const validate = () => {
    const errors = validatePasswordChange(values());

    fields.currentPassword.field.setError(errors?.currentPassword ?? '');
    fields.newPassword.field.setError(errors?.newPassword ?? '');
    fields.verifyNewPassword.field.setError(errors?.verifyNewPassword ?? errors?.passwordMismatch ?? '');

    updateButtons(errors);

    return errors === null;
  };

  const clearForm = () => {
    for (const { input, field } of Object.values(fields)) {
      input.value = '';
      field.setError('');
    }

    touched.value = false;
    updateButtons();
  };

  const setDisabled = (disabled) => {
    for (const control of controls) control.disabled = disabled;
  };

  for (const { input } of Object.values(fields)) {
    input.addEventListener('input', () => {
      touched.value = true;
      validate();
    });
  }

  reset.addEventListener('click', () => clearForm());

  form.addEventListener('submit', async (event) => {
    event.preventDefault();

    if (!validate()) return;

    setDisabled(true);

    const changed = await getAdminContext().useCases.changePassword(
      fields.currentPassword.input.value,
      fields.newPassword.input.value,
    );

    if (changed) {
      showSnackbar('Password changed successfully');
      clearForm();
    } else {
      showSnackbar('Unable to change password');
    }

    setDisabled(false);
    updateButtons();
  });

  details.append(help, form);

  return details;
}
