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

import { Component, EventEmitter, Output } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';
import { RdioScannerAdminService } from '../admin.service';

@Component({
    selector: 'rdio-scanner-admin-login',
    styleUrls: ['./login.component.scss'],
    templateUrl: './login.component.html',
})
export class RdioScannerAdminLoginComponent {
    @Output() loggedIn = new EventEmitter<void>();

    form: FormGroup;

    message = '';

    constructor(
        private adminService: RdioScannerAdminService,
        private ngFormBuilder: FormBuilder,
    ) {
        this.form = this.ngFormBuilder.group({
            password: this.ngFormBuilder.control(null, Validators.required),
        });
    }

    async login(password = this.form.get('password')?.value): Promise<void> {
        if (!password) {
            return;
        }

        this.form.disable();

        const loggedIn = await this.adminService.login(password);

        if (loggedIn) {
            this.loggedIn.emit();

        } else {
            this.form.enable();
            this.form.reset();

            this.message = 'Invalid password';
        }
    }
}
