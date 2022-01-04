/*
 * *****************************************************************************
 * Copyright (C) 2019-2022 Chrystian Huot <chrystian.huot@saubeo.solutions>
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

import { ChangeDetectionStrategy, ChangeDetectorRef, Component, OnDestroy, OnInit, QueryList, ViewChildren, ViewEncapsulation } from '@angular/core';
import { FormArray, FormControl, FormGroup } from '@angular/forms';
import { MatExpansionPanel } from '@angular/material/expansion';
import { AdminEvent, RdioScannerAdminService, Config } from '../admin.service';

@Component({
    changeDetection: ChangeDetectionStrategy.OnPush,
    encapsulation: ViewEncapsulation.None,
    selector: 'rdio-scanner-admin-config',
    styleUrls: ['./config.component.scss'],
    templateUrl: './config.component.html',
})
export class RdioScannerAdminConfigComponent implements OnDestroy, OnInit {
    docker: boolean = false;

    form: FormGroup | undefined;

    get access(): FormArray {
        return this.form?.get('access') as FormArray;
    }

    get apiKeys(): FormArray {
        return this.form?.get('apiKeys') as FormArray;
    }

    get dirWatch(): FormArray {
        return this.form?.get('dirWatch') as FormArray;
    }

    get downstreams(): FormArray {
        return this.form?.get('downstreams') as FormArray;
    }

    get groups(): FormArray {
        return this.form?.get('groups') as FormArray;
    }

    get options(): FormGroup {
        return this.form?.get('options') as FormGroup;
    }

    get systems(): FormArray {
        return this.form?.get('systems') as FormArray;
    }

    get tags(): FormArray {
        return this.form?.get('tags') as FormArray;
    }

    private config: Config | undefined;

    private eventSubscription = this.adminService.event.subscribe(async (event: AdminEvent) => {
        if ('authenticated' in event && event.authenticated === true) {
            this.config = await this.adminService.getConfig();

            this.reset();
        }

        if ('config' in event) {
            this.config = event.config;

            if (this.form?.pristine) {
                this.reset();
            }
        }

        if ('docker' in event) {
            this.docker = event.docker!;
        }
    });

    @ViewChildren(MatExpansionPanel) private panels: QueryList<MatExpansionPanel> | undefined;

    constructor(
        private adminService: RdioScannerAdminService,
        private ngChangeDetectorRef: ChangeDetectorRef,
    ) { }

    ngOnDestroy(): void {
        this.eventSubscription.unsubscribe();
    }

    async ngOnInit(): Promise<void> {
        this.config = await this.adminService.getConfig();

        this.reset();
    }

    closeAll(): void {
        this.panels?.forEach((panel) => panel.close());
    }

    reset(config = this.config, options?: { dirty?: boolean }): void {
        this.form = this.adminService.newConfigForm(config);

        this.form.statusChanges.subscribe(() => {
            this.ngChangeDetectorRef.markForCheck();
        });

        this.groups.valueChanges.subscribe(() => {
            this.systems.controls.forEach((system) => {
                const talkgroups = system.get('talkgroups') as FormArray;

                talkgroups.controls.forEach((talkgroup) => {
                    const groupId = talkgroup.get('groupId') as FormControl;

                    groupId.updateValueAndValidity({ onlySelf: true });

                    if (groupId.errors) {
                        groupId.markAsTouched({ onlySelf: true });
                    }
                });
            });
        });

        this.tags.valueChanges.subscribe(() => {
            this.systems.controls.forEach((system) => {
                const talkgroups = system.get('talkgroups') as FormArray;

                talkgroups.controls.forEach((talkgroup) => {
                    const tagId = talkgroup.get('tagId') as FormControl;

                    tagId.updateValueAndValidity({ onlySelf: true });

                    if (tagId.errors) {
                        tagId.markAsTouched({ onlySelf: true });
                    }
                });
            });
        });

        if (options?.dirty === true) {
            this.form.markAsDirty();
        }

        this.ngChangeDetectorRef.markForCheck();
    }

    async save(): Promise<void> {
        this.form?.markAsPristine();

        await this.adminService.saveConfig(this.form?.getRawValue());
    }
}
