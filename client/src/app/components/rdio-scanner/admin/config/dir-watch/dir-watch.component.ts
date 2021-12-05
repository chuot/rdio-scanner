/*
 * *****************************************************************************
 * Copyright (C) 2019-2021 Chrystian Huot <chrystian.huot@saubeo.solutions>
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
import { Component, Input, OnChanges, QueryList, ViewChildren } from '@angular/core';
import { FormArray, FormControl, FormGroup } from '@angular/forms';
import { MatExpansionPanel } from '@angular/material/expansion';
import { RdioScannerAdminService } from '../../admin.service';

@Component({
    selector: 'rdio-scanner-admin-dir-watch',
    templateUrl: './dir-watch.component.html',
})
export class RdioScannerAdminDirWatchComponent implements OnChanges {
    @Input() form: FormArray | undefined;

    get dirWatches(): FormGroup[] {
        return this.form?.controls
            .sort((a, b) => a.value.order - b.value.order) as FormGroup[];
    }

    get systems(): FormGroup[] {
        const systems = this.form?.root.get('systems') as FormArray;

        return systems.controls as FormGroup[];
    }

    get talkgroups(): FormGroup[][] {
        return this.systems.reduce((talkgroups, system) => {
            const faTalkgroups = system.get('talkgroups') as FormArray;

            talkgroups[system.value.id] = faTalkgroups.controls as FormGroup[];

            return talkgroups;
        }, [] as FormGroup[][]);
    }

    @ViewChildren(MatExpansionPanel) private panels: QueryList<MatExpansionPanel> | undefined;

    constructor(private adminService: RdioScannerAdminService) { }

    ngOnChanges(): void {
        if (this.form) {
            this.dirWatches.flatMap((control) => this.registerOnChanges(control));
        }
    }

    add(): void {
        const dirWatch = this.adminService.newDirWatchForm();

        dirWatch.markAllAsTouched();

        this.registerOnChanges(dirWatch);

        this.form?.insert(0, dirWatch);

        this.form?.markAsDirty();
    }

    closeAll(): void {
        this.panels?.forEach((panel) => panel.close());
    }

    drop(event: CdkDragDrop<FormGroup[]>): void {
        if (event.previousIndex !== event.currentIndex) {
            moveItemInArray(event.container.data, event.previousIndex, event.currentIndex);

            event.container.data.forEach((dat, idx) => dat.get('order')?.setValue(idx + 1, { emitEvent: false }));

            this.form?.markAsDirty();
        }
    }

    remove(index: number): void {
        this.form?.removeAt(index);

        this.form?.markAsDirty();
    }

    private registerOnChanges(control: FormGroup): void {
        const mask = control.get('mask') as FormControl;
        const type = control.get('type') as FormControl;

        mask.valueChanges.subscribe(() => this.validateIds(control));
        type.valueChanges.subscribe(() => this.validateIds(control));
    }

    private validateIds(control: FormGroup): void {
        const systemId = control.get('systemId');
        const talkgroupId = control.get('talkgroupId');

        systemId?.updateValueAndValidity();
        systemId?.markAsTouched();

        talkgroupId?.updateValueAndValidity();
        talkgroupId?.markAsTouched();
    }
}
