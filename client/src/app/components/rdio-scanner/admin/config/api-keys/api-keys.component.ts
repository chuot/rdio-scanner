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
import { Component, Input, QueryList, ViewChildren } from '@angular/core';
import { MatDialog } from '@angular/material/dialog';
import { FormArray, FormGroup } from '@angular/forms';
import { MatExpansionPanel } from '@angular/material/expansion';
import { RdioScannerAdminService } from '../../admin.service';
import { RdioScannerAdminSystemsSelectComponent } from '../systems/select/select.component';

@Component({
    selector: 'rdio-scanner-admin-api-keys',
    templateUrl: './api-keys.component.html',
})
export class RdioScannerAdminApiKeysComponent {
    @Input() form: FormArray | undefined;

    get apiKeys(): FormGroup[] {
        return this.form?.controls
            .sort((a, b) => a.value.order - b.value.order) as FormGroup[];
    }

    @ViewChildren(MatExpansionPanel) private panels: QueryList<MatExpansionPanel> | undefined;

    constructor(private adminService: RdioScannerAdminService, private matDialog: MatDialog) { }

    add(): void {
        const apiKey = this.adminService.newApiKeyForm({
            key: this.uuid(),
            systems: '*',
        });

        apiKey.markAllAsTouched();

        this.form?.insert(0, apiKey);

        this.form?.markAsDirty();
    }

    closeAll(): void {
        this.panels?.forEach((panel) => panel.close());
    }

    copy(inputElement: HTMLInputElement): void {
        inputElement.select();

        document.execCommand('copy');

        inputElement.setSelectionRange(0, 0);
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

    select(access: FormGroup): void {
        const matDialogRef = this.matDialog.open(RdioScannerAdminSystemsSelectComponent, { data: access });

        matDialogRef.afterClosed().subscribe((data) => {
            if (data) {
                access.get('systems')?.setValue(data);

                access.markAsDirty();
            }
        });
    }

    private uuid() {
        let dt = new Date().getTime();

        return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function (c) {
            const r = (dt + Math.random() * 16) % 16 | 0;

            dt = Math.floor(dt / 16);

            return (c == 'x' ? r : (r & 0x3 | 0x8)).toString(16);
        });
    }
}
