// admin password change validation:
// current and new password required, new at least 8, confirmation matches.

export function validatePasswordChange({ currentPassword, newPassword, verifyNewPassword } = {}) {
  const errors = {};

  if (!currentPassword) errors.currentPassword = 'Current password is required';

  if (!newPassword) errors.newPassword = 'New password is required';
  else if (newPassword.length < 8) errors.newPassword = 'Password is too short';

  if (!verifyNewPassword) errors.verifyNewPassword = 'New password verification is required';

  if (newPassword && verifyNewPassword && newPassword !== verifyNewPassword) errors.passwordMismatch = "Passwords don't match";

  return Object.keys(errors).length ? errors : null;
}
