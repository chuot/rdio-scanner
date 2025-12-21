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

import { CdkDragDrop, moveItemInArray } from '@angular/cdk/drag-drop';
import { Component, Input, QueryList, ViewChildren, OnDestroy } from '@angular/core';
import { MatDialog } from '@angular/material/dialog';
import { FormArray, FormGroup } from '@angular/forms';
import { MatExpansionPanel } from '@angular/material/expansion';
import { MatSelectChange } from '@angular/material/select';
import { RdioScannerAdminService } from '../../admin.service';
import { Subscription } from 'rxjs';

@Component({
    selector: 'rdio-scanner-admin-groups',
    templateUrl: './groups.component.html',
    standalone: false
})
export class RdioScannerAdminGroupsComponent implements OnDestroy {
    private _form: FormArray | undefined;
    private formSub: Subscription | undefined;
    private cachedGroups: FormGroup[] | undefined;
    private cacheValid = false;
    readonly leds: string[] = [];
    readonly alertsArray: string[] = [];

    @Input()
    set form(value: FormArray | undefined) {
        this._form = value;
        this.cacheValid = false;
        this.cachedGroups = undefined;
        this.formSub?.unsubscribe();
        if (this._form) {
            this.formSub = this._form.valueChanges.subscribe(() => {
                this.cacheValid = false;
            });
        }
    }
    get form(): FormArray | undefined {
        return this._form;
    }

    get alerts(): string[] {
        return this.alertsArray;
    }

    get groups(): FormGroup[] {
        if (this.cacheValid && this.cachedGroups) return this.cachedGroups;
        const controls = (this._form?.controls?.slice() || []) as FormGroup[];
        this.cachedGroups = controls.sort((a, b) => (a.value.order || 0) - (b.value.order || 0));
        this.cacheValid = true;
        return this.cachedGroups;
    }

    @ViewChildren(MatExpansionPanel) private panels: QueryList<MatExpansionPanel> | undefined;

    constructor(private adminService: RdioScannerAdminService, private matDialog: MatDialog) {
        this.leds = this.adminService.getLeds();
        this.alertsArray = Object.keys(this.adminService.Alerts || {});
    }

    add(): void {
        const group = this.adminService.newGroupForm();

        group.markAllAsTouched();

        this.form?.insert(0, group);

        this.form?.markAsDirty();
        this.cacheValid = false;
    }

    closeAll(): void {
        this.panels?.forEach((panel) => panel.close());
    }

    drop(event: CdkDragDrop<FormGroup[]>): void {
        if (event.previousIndex !== event.currentIndex) {
            moveItemInArray(event.container.data, event.previousIndex, event.currentIndex);

            event.container.data.forEach((dat, idx) => dat.get('order')?.setValue(idx + 1, { emitEvent: false }));

            this.form?.markAsDirty();
            this.cacheValid = false;
        }
    }

    async playAlert(event: MatSelectChange): Promise<void> {
        if (event.value) await this.adminService.playAlert(event.value);
    }

    remove(index: number): void {
        this.form?.removeAt(index);

        this.form?.markAsDirty();
        this.cacheValid = false;
    }

    ngOnDestroy(): void {
        this.formSub?.unsubscribe();
    }
}
