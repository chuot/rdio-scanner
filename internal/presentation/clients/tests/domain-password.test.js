// T045: admin password change validation (domain/password.js).
// Red first: these tests define the validator contract mirrored from the
// legacy tools/password Angular validators (required, minLength 8, match).

import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { validatePasswordChange } from '../admin/domain/password.js';

const valid = {
  currentPassword: 'old-secret',
  newPassword: 'new-secret',
  verifyNewPassword: 'new-secret',
};

describe('domain/password — validatePasswordChange', () => {
  it('returns null for a valid change', () => {
    assert.equal(validatePasswordChange(valid), null);
  });

  it('requires the current password', () => {
    assert.deepEqual(validatePasswordChange({ ...valid, currentPassword: '' }), {
      currentPassword: 'Current password is required',
    });
  });

  it('requires the new password', () => {
    assert.deepEqual(validatePasswordChange({ ...valid, newPassword: '', verifyNewPassword: '' }), {
      newPassword: 'New password is required',
      verifyNewPassword: 'New password verification is required',
    });
  });

  it('rejects a new password shorter than 8 characters', () => {
    assert.deepEqual(validatePasswordChange({ ...valid, newPassword: '1234567', verifyNewPassword: '1234567' }), {
      newPassword: 'Password is too short',
    });
  });

  it('accepts a new password of exactly 8 characters (boundary)', () => {
    assert.equal(validatePasswordChange({ ...valid, newPassword: '12345678', verifyNewPassword: '12345678' }), null);
  });

  it('requires the new password verification', () => {
    assert.deepEqual(validatePasswordChange({ ...valid, verifyNewPassword: '' }), {
      verifyNewPassword: 'New password verification is required',
    });
  });

  it('reports the mismatch only when both new passwords are present and unequal', () => {
    assert.deepEqual(validatePasswordChange({ ...valid, verifyNewPassword: 'other-secret' }), {
      passwordMismatch: "Passwords don't match",
    });
  });

  it('does not report a mismatch when the verification is missing', () => {
    const errors = validatePasswordChange({ ...valid, verifyNewPassword: '' });

    assert.deepEqual(errors, { verifyNewPassword: 'New password verification is required' });
    assert.equal(errors.passwordMismatch, undefined);
  });

  it('does not report a mismatch when the new password is missing', () => {
    const errors = validatePasswordChange({ ...valid, newPassword: '', verifyNewPassword: 'x' });

    assert.equal(errors.passwordMismatch, undefined);
  });

  it('collects every error at once', () => {
    assert.deepEqual(validatePasswordChange({ currentPassword: '', newPassword: 'short', verifyNewPassword: '' }), {
      currentPassword: 'Current password is required',
      newPassword: 'Password is too short',
      verifyNewPassword: 'New password verification is required',
    });
  });

  it('treats missing fields as required errors', () => {
    assert.deepEqual(validatePasswordChange({}), {
      currentPassword: 'Current password is required',
      newPassword: 'New password is required',
      verifyNewPassword: 'New password verification is required',
    });
  });
});
