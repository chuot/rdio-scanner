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

import { Component, EventEmitter, Input, Output, ChangeDetectionStrategy, ChangeDetectorRef } from '@angular/core';
import { FormGroup, AbstractControl } from '@angular/forms';

@Component({
    selector: 'rdio-scanner-admin-site',
    templateUrl: './site.component.html',
    standalone: false,
    changeDetection: ChangeDetectionStrategy.OnPush
})
export class RdioScannerAdminSiteComponent {
    private _form: FormGroup | undefined;

    @Input()
    set form(f: FormGroup | undefined) {
        this._form = f;
        this.cd.markForCheck();
    }
    get form(): FormGroup | undefined {
        return this._form;
    }

    get siteRef(): AbstractControl | null | undefined {
        return this._form?.get('siteRef');
    }

    get labelControl(): AbstractControl | null | undefined {
        return this._form?.get('label');
    }

    @Output() remove = new EventEmitter<void>();

    constructor(private readonly cd: ChangeDetectorRef) {}
}
