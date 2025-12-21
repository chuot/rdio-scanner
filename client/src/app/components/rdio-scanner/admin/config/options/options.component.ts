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

import { Component, Input, ChangeDetectionStrategy } from '@angular/core';
import { FormGroup } from '@angular/forms';

@Component({
    selector: 'rdio-scanner-admin-options',
    templateUrl: './options.component.html',
    standalone: false,
    changeDetection: ChangeDetectionStrategy.OnPush
})
export class RdioScannerAdminOptionsComponent {
    @Input() form: FormGroup | null = null;

    get controls() {
        return this.form ? this.form.controls : {};
    }
}
