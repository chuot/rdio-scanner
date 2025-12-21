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
import { Component, Input, QueryList, ViewChildren, ChangeDetectionStrategy, ChangeDetectorRef, OnDestroy } from '@angular/core';
import { FormArray, FormGroup } from '@angular/forms';
import { MatExpansionPanel } from '@angular/material/expansion';
import { RdioScannerAdminService } from '../../admin.service';
import { Subscription } from 'rxjs';

@Component({
    selector: 'rdio-scanner-admin-systems',
    templateUrl: './systems.component.html',
    standalone: false,
    changeDetection: ChangeDetectionStrategy.OnPush
})
export class RdioScannerAdminSystemsComponent implements OnDestroy {
    private _form: FormArray | undefined;
    private subs = new Subscription();
    private _systemsCache: FormGroup[] = [];

    @Input()
    set form(f: FormArray | undefined) {
        if (this._form === f) { return; }
        this.subs.unsubscribe();
        this.subs = new Subscription();
        this._form = f;
        this.updateCache();
        if (this._form) {
            const s = this._form.valueChanges.subscribe(() => this.updateCache());
            this.subs.add(s);
        }
    }
    get form(): FormArray | undefined { return this._form; }

    get systems(): FormGroup[] {
        return this._systemsCache;
    }

    @ViewChildren(MatExpansionPanel) private panels: QueryList<MatExpansionPanel> | undefined;

    constructor(private adminService: RdioScannerAdminService, private cd: ChangeDetectorRef) { }

    add(): void {
        const system = this.adminService.newSystemForm();

        system.markAllAsTouched();

        this.form?.insert(0, system);

        this.form?.markAsDirty();

        this.updateCache();
    }

    closeAll(): void {
        this.panels?.forEach((panel) => panel.close());
    }

    drop(event: CdkDragDrop<FormGroup[]>): void {
        if (event.previousIndex !== event.currentIndex) {
            moveItemInArray(event.container.data, event.previousIndex, event.currentIndex);

            event.container.data.forEach((dat, idx) => dat.get('order')?.setValue(idx + 1, { emitEvent: false }));

            this.form?.markAsDirty();

            this.updateCache();
        }
    }

    remove(index: number): void {
        this.form?.removeAt(index);

        this.form?.markAsDirty();

        this.updateCache();
    }

    private updateCache(): void {
        if (!this._form) {
            this._systemsCache = [];
            this.cd.markForCheck();
            return;
        }
        const arr = [...this._form.controls] as FormGroup[];
        arr.sort((a, b) => (a.value.order || 0) - (b.value.order || 0));
        this._systemsCache = arr;
        this.cd.markForCheck();
    }

    ngOnDestroy(): void {
        this.subs.unsubscribe();
    }
}
