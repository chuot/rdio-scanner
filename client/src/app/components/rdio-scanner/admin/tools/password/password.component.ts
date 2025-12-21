/*
 * *****************************************************************************
 * Copyright (C) 2019-2026 Chrystian Huot <chrystian@huot.qc.ca>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>
 * ****************************************************************************
 */

import { Component } from '@angular/core';
import { AbstractControl, FormBuilder, FormGroup, ValidationErrors, Validators } from '@angular/forms';
import { MatSnackBar } from '@angular/material/snack-bar';
import { RdioScannerAdminService } from '../../admin.service';

@Component({
    selector: 'rdio-scanner-admin-password',
    styleUrls: ['./password.component.scss'],
    templateUrl: './password.component.html',
    standalone: false
})
export class RdioScannerAdminPasswordComponent {
    form: FormGroup;

    constructor(
        private readonly adminService: RdioScannerAdminService,
        private readonly matSnackBar: MatSnackBar,
        private readonly ngFormBuilder: FormBuilder,
    ) {
        this.form = this.ngFormBuilder.group({
            currentPassword: [null, Validators.required],
            newPassword: [null, [Validators.required, Validators.minLength(8)]],
            verifyNewPassword: [null, Validators.required],
        }, { validators: this.passwordsMatchValidator });

        this.form.get('newPassword')?.valueChanges.subscribe(() =>
            this.form.get('verifyNewPassword')?.updateValueAndValidity({ onlySelf: true })
        );
    }

    private passwordsMatchValidator(group: AbstractControl): ValidationErrors | null {
        const newPwd = group.get('newPassword')?.value;
        const verify = group.get('verifyNewPassword')?.value;
        return newPwd && verify && newPwd !== verify ? { passwordMismatch: true } : null;
    }

    get currentPassword() { return this.form.get('currentPassword'); }
    get newPassword() { return this.form.get('newPassword'); }
    get verifyNewPassword() { return this.form.get('verifyNewPassword'); }

    reset(): void {
        this.form.reset();
    }

    async save(): Promise<void> {
        if (this.form.invalid) {
            this.form.markAllAsTouched();
            return;
        }

        const config = { duration: 5000 };
        const currentPassword = this.currentPassword?.value;
        const newPassword = this.newPassword?.value;

        this.form.disable();
        try {
            await this.adminService.changePassword(currentPassword, newPassword);
            this.matSnackBar.open('Password changed successfully', '', config);
            this.form.reset();
        } catch {
            this.matSnackBar.open('Unable to change password', '', config);
        } finally {
            this.form.enable();
        }
    }
}
