/*
 * *****************************************************************************
 * Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
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

import { Component, Input } from '@angular/core';
import { FormArray, FormGroup } from '@angular/forms';
import { RdioScannerAdminService } from '../../admin.service';

@Component({
    selector: 'rdio-scanner-admin-groups',
    styleUrls: ['./groups.component.scss'],
    templateUrl: './groups.component.html',
})
export class RdioScannerAdminGroupsComponent {
    @Input() form: FormArray | undefined;

    get groups(): FormGroup[] {
        return this.form?.controls
            .sort((a, b) => (a.value.label || '').localeCompare(b.value.label || '')) as FormGroup[];
    }

    constructor(private adminService: RdioScannerAdminService) { }

    add(): void {
        const id = this.groups.reduce((pv, cv) => cv.value._id >= pv ? cv.value._id + 1 : pv, 0);

        const group = this.adminService.newGroupForm({ _id: id });

        group.markAllAsTouched();

        this.form?.insert(0, group);

        this.form?.markAsDirty();
    }

    remove(index: number): void {
        this.form?.removeAt(index);

        this.form?.markAsDirty();
    }
}
