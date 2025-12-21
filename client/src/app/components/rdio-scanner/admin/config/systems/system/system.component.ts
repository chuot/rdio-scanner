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
import { Component, EventEmitter, Input, Output, QueryList, ViewChildren } from '@angular/core';
import { FormArray, FormControl, FormGroup } from '@angular/forms';
import { MatExpansionPanel } from '@angular/material/expansion';
import { MatSelectChange } from '@angular/material/select';
import { RdioScannerAdminService, Group, Tag } from '../../../admin.service';

@Component({
    selector: 'rdio-scanner-admin-system',
    templateUrl: './system.component.html',
    standalone: false
})
export class RdioScannerAdminSystemComponent {
    @Input() form = new FormGroup({});

    @Input() groups: Group[] = [];

    @Input() tags: Tag[] = [];

    @Output() add = new EventEmitter<void>();

    @Output() remove = new EventEmitter<void>();

    leds: string[];

    get alerts(): string[] {
        return Object.keys(this.adminService.Alerts || {});
    }

    get sites(): FormGroup[] {
        const sites = this.form.get('sites') as FormArray | null;
        return (sites?.controls as FormGroup[]) || [];
    }

    get talkgroups(): FormGroup[] {
        const talkgroups = this.form.get('talkgroups') as FormArray | null;
        return (talkgroups?.controls as FormGroup[]) || [];
    }

    get units(): FormGroup[] {
        const units = this.form.get('units') as FormArray | null;
        return (units?.controls as FormGroup[]) || [];
    }

    // trackBy to preserve identity in ngFor lists
    trackById(_index: number, group: FormGroup): any {
        return group.get('id')?.value ?? _index;
    }

    @ViewChildren(MatExpansionPanel) private panels: QueryList<MatExpansionPanel> | undefined;

    constructor(private adminService: RdioScannerAdminService) {
        this.leds = this.adminService.getLeds();
    }

    addSite(): void {
        const sites = this.form.get('sites') as FormArray | null;

        sites?.insert(0, this.adminService.newSiteForm());

        this.form.markAsDirty();
    }

    addTalkgroup(): void {
        const talkgroups = this.form.get('talkgroups') as FormArray | null;

        talkgroups?.insert(0, this.adminService.newTalkgroupForm());

        this.form.markAsDirty();
    }

    addUnit(): void {
        const units = this.form.get('units') as FormArray | null;

        units?.insert(0, this.adminService.newUnitForm());

        this.form.markAsDirty();
    }

    blacklistTalkgroup(index: number): void {
        const talkgroup = this.talkgroups[index];

        const id = talkgroup.value.id;

        if (typeof id !== 'number') {
            return;
        }

        const blacklists = this.form.get('blacklists') as FormControl | null;

        blacklists?.setValue(blacklists.value?.trim() ? `${blacklists.value},${id}` : `${id}`);

        this.removeTalkgroup(index);
    }

    closeAll(): void {
        this.panels?.forEach((panel) => panel.close());
    }

    drop(event: CdkDragDrop<FormGroup[]>): void {
        if (event.previousIndex !== event.currentIndex) {
            moveItemInArray(event.container.data, event.previousIndex, event.currentIndex);

            event.container.data.forEach((dat, idx) => dat.get('order')?.setValue(idx + 1, { emitEvent: false }));

            this.form.markAsDirty();
        }
    }

    async playAlert(event: MatSelectChange): Promise<void> {
        if (event.value) await this.adminService.playAlert(event.value);
    }

    removeSite(index: number): void {
        const sites = this.form.get('sites') as FormArray | null;

        sites?.removeAt(index);

        sites?.markAsDirty();
    }

    removeTalkgroup(index: number): void {
        const talkgroups = this.form.get('talkgroups') as FormArray | null;

        talkgroups?.removeAt(index);

        talkgroups?.markAsDirty();
    }

    removeUnit(index: number): void {
        const units = this.form.get('units') as FormArray | null;

        units?.removeAt(index);

        units?.markAsDirty();
    }
}
