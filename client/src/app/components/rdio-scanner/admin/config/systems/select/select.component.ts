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

import { Component, Inject, ViewEncapsulation, OnDestroy } from '@angular/core';
import { FormArray, FormBuilder, FormControl, FormGroup } from '@angular/forms';
import { MAT_DIALOG_DATA, MatDialogRef } from '@angular/material/dialog';
import { Access } from '../../../admin.service';
import { Subscription } from 'rxjs';

interface System {
    all: boolean;
    id: number;
    talkgroups: Talkgroup[];
}

interface Talkgroup {
    checked: boolean;
    id: number;
}

@Component({
    encapsulation: ViewEncapsulation.None,
    selector: 'rdio-scanner-admin-systems-selection',
    styleUrls: ['./select.component.scss'],
    templateUrl: './select.component.html',
    standalone: false
})
export class RdioScannerAdminSystemsSelectComponent implements OnDestroy {
    indeterminate = {
        everything: false,
        groups: [] as boolean[],
        systems: [] as boolean[],
        tags: [] as boolean[],
    };

    select: FormGroup;

    configTalkgroups: FormGroup[][];

    get configGroups(): FormGroup[] {
        const faGroups = this.access.root.get('groups') as FormArray;
        return faGroups.controls as FormGroup[];
    }

    get configSystems(): FormGroup[] {
        const faSystems = this.access.root.get('systems') as FormArray;
        return faSystems.controls as FormGroup[];
    }

    get configTags(): FormGroup[] {
        const faTags = this.access.root.get('tags') as FormArray;
        return faTags.controls as FormGroup[];
    }

    private subs = new Subscription();

    constructor(
        @Inject(MAT_DIALOG_DATA) public access: FormGroup,
        private matDialogRef: MatDialogRef<RdioScannerAdminSystemsSelectComponent>,
        private ngFormBuilder: FormBuilder,
    ) {
        this.configTalkgroups = this.configSystems.map((fgSystem) => {
            const faTalkgroups = fgSystem.get('talkgroups') as FormArray;
            return faTalkgroups.controls as FormGroup[];
        });

        this.select = this.ngFormBuilder.group({
            all: this.ngFormBuilder.nonNullable.control(false),
            groups: this.ngFormBuilder.nonNullable.array<FormGroup>([]),
            tags: this.ngFormBuilder.nonNullable.array<FormGroup>([]),
            systems: this.ngFormBuilder.nonNullable.array<FormGroup>([]),
        });

        const fcAll = this.select.get('all') as FormControl;
        const faGroups = this.select.get('groups') as FormArray;
        const faSystems = this.select.get('systems') as FormArray;
        const faTags = this.select.get('tags') as FormArray;

        for (const configGroup of this.configGroups) {
            const fgGroup = this.ngFormBuilder.group({
                id: this.ngFormBuilder.control(configGroup.get('id')?.value),
                checked: this.ngFormBuilder.control(false),
            });
            faGroups.push(fgGroup);
            this.subs.add(fgGroup.valueChanges.subscribe((vGroup) => {
                for (const fgSystem of faSystems.controls) {
                    const faTalkgroups = fgSystem.get('talkgroups') as FormArray;
                    for (const fgTalkgroup of faTalkgroups.controls) {
                        const ids = fgTalkgroup.get('groupIds')?.value;
                        if (ids && ids.includes(vGroup.id) && fgTalkgroup.get('checked')?.value !== vGroup.checked) {
                            fgTalkgroup.get('checked')?.setValue(vGroup.checked);
                        }
                    }
                }
            }));
        }

        for (let index = 0; index < this.configSystems.length; index++) {
            const configSystem = this.configSystems[index];
            const fcSystemAll = this.ngFormBuilder.control(false);
            const faSystemTalkgroups = this.ngFormBuilder.array<FormGroup>([]);
            const fgSystem = this.ngFormBuilder.group({
                all: fcSystemAll,
                id: this.ngFormBuilder.control(configSystem.get('systemRef')?.value),
                talkgroups: faSystemTalkgroups
            });

            for (const configTalkgroup of this.configTalkgroups[index]) {
                const fgSystemTalkgroup = this.ngFormBuilder.group({
                    checked: this.ngFormBuilder.nonNullable.control(false),
                    groupIds: this.ngFormBuilder.nonNullable.control(configTalkgroup.get('groupIds')?.value),
                    id: this.ngFormBuilder.nonNullable.control(configTalkgroup.get('talkgroupRef')?.value),
                    tagId: this.ngFormBuilder.nonNullable.control(configTalkgroup.get('tagId')?.value),
                });
                faSystemTalkgroups.push(fgSystemTalkgroup);
                this.subs.add(fgSystemTalkgroup.valueChanges.subscribe(() => {
                    const vAll = faSystemTalkgroups.controls.every((t) => t.get('checked')?.value);
                    fcSystemAll.setValue(vAll, { emitEvent: false });
                }));
            }

            faSystems.push(fgSystem);

            this.subs.add(fgSystem.valueChanges.subscribe(() => {
                this.rebuildGroupIndeterminates();
                this.rebuildTagIndeterminates();
            }));

            this.subs.add(fcSystemAll.valueChanges.subscribe((vAll) => {
                const faTalkgroups = fgSystem.get('talkgroups') as FormArray;
                for (const fgTalkgroup of faTalkgroups.controls) {
                    fgTalkgroup.get('checked')?.setValue(vAll);
                }
                this.rebuildGroupIndeterminates();
                this.rebuildTagIndeterminates();
            }));

            this.subs.add(faSystemTalkgroups.valueChanges.subscribe((vSystemTalkgroups) => {
                let on = 0;
                let off = 0;
                for (const v of vSystemTalkgroups) {
                    if (v.checked) on++; else off++;
                }
                this.indeterminate.systems[index] = !!off && !!on;
                faSystems.at(index).get('all')?.setValue(!off && on, { emitEvent: false });
            }));
        }

        for (const configTag of this.configTags) {
            const fgTag = this.ngFormBuilder.group({
                id: this.ngFormBuilder.control(configTag.value.id),
                checked: this.ngFormBuilder.control(false),
            });
            faTags.push(fgTag);
            this.subs.add(fgTag.valueChanges.subscribe((vTag) => {
                for (const fgSystem of faSystems.controls) {
                    const faTalkgroups = fgSystem.get('talkgroups') as FormArray;
                    for (const fgTalkgroup of faTalkgroups.controls) {
                        if (fgTalkgroup.value.tagId === vTag.id && fgTalkgroup.value.checked !== vTag.checked) {
                            fgTalkgroup.get('checked')?.setValue(vTag.checked);
                        }
                    }
                }
            }));
        }

        this.subs.add(fcAll.valueChanges.subscribe((vAll) => {
            for (const fgSystem of faSystems.controls) {
                const faTalkgroups = fgSystem.get('talkgroups') as FormArray;
                for (const fgTalkgroup of faTalkgroups.controls) {
                    fgTalkgroup.get('checked')?.setValue(vAll);
                }
            }
            for (const fg of faGroups.controls) fg.get('checked')?.setValue(vAll);
            for (const fg of faTags.controls) fg.get('checked')?.setValue(vAll);
        }));

        this.subs.add(faSystems.valueChanges.subscribe((vSystems: System[]) => {
            let on = 0;
            let off = 0;
            for (const vSystem of vSystems) {
                if (vSystem.all) on++; else off++;
            }
            this.indeterminate.everything = !!off && !!on;
            fcAll.setValue(!off && on, { emitEvent: false });
        }));

        const vAccess: Access = this.access.value;

        if (vAccess.systems === '*') {
            this.select.get('all')?.setValue(true);
        } else if (Array.isArray(vAccess.systems)) {
            for (const vSystem of vAccess.systems) {
                if (typeof vSystem === 'number') {
                    faSystems.controls.find((fgSystem) => fgSystem.get('id')?.value === vSystem)?.get('all')?.setValue(true);
                } else if (vSystem && typeof vSystem === 'object') {
                    const fgSystem = faSystems.controls.find((fg) => fg.get('id')?.value === vSystem.id);
                    if (fgSystem) {
                        if (vSystem.talkgroups === '*') {
                            fgSystem.get('all')?.setValue(true);
                        } else if (Array.isArray(vSystem.talkgroups)) {
                            const faTalkgroups = fgSystem.get('talkgroups') as FormArray;
                            for (const talkgroup of vSystem.talkgroups) {
                                const talkgroupId = typeof talkgroup === 'number' ? talkgroup : talkgroup.id;
                                const fgTalkgroup = faTalkgroups.controls.find((fg) => fg.get('id')?.value === talkgroupId);
                                fgTalkgroup?.get('checked')?.setValue(true);
                            }
                            fgSystem.updateValueAndValidity();
                        }
                    }
                }
            }
        }
    }

    accept(): void {
        const access: Access['systems'] = this.select.get('all')?.value ? '*' : this.select.get('systems')?.value.filter((system: System) => {
            return system['all'] || system['talkgroups'].some((talkgroup: Talkgroup) => talkgroup.checked);
        }).map((system: System) => {
            if (system['all']) {
                return { id: system['id'], talkgroups: '*' };
            } else {
                return {
                    id: system['id'],
                    talkgroups: system['talkgroups']
                        .filter((talkgroup: Talkgroup) => talkgroup.checked)
                        .map((talkgroup: Talkgroup) => talkgroup.id),
                };
            }
        });

        this.matDialogRef.close(access);
    }

    cancel(): void {
        this.matDialogRef.close(null);
    }

    private rebuildGroupIndeterminates(): void {
        const faGroups = this.select.get('groups') as FormArray;
        const faSystems = this.select.get('systems') as FormArray;

        for (let index = 0; index < faGroups.length; index++) {
            const fgGroup = faGroups.at(index);
            let on = 0;
            let off = 0;
            for (const fgSystem of faSystems.controls) {
                const faTalkgroups = fgSystem.get('talkgroups') as FormArray;
                for (const fgTalkgroup of faTalkgroups.controls) {
                    if (fgTalkgroup.get('groupIds')?.value.includes(fgGroup.get('id')?.value)) {
                        if (fgTalkgroup.get('checked')?.value) on++; else off++;
                    }
                }
            }
            this.indeterminate.groups[index] = !!off && !!on;
            fgGroup.get('checked')?.setValue(!off && on, { emitEvent: false });
        }
    }

    private rebuildTagIndeterminates(): void {
        const faTags = this.select.get('tags') as FormArray;
        const faSystems = this.select.get('systems') as FormArray;

        for (let index = 0; index < faTags.length; index++) {
            const fgTag = faTags.at(index);
            let on = 0;
            let off = 0;
            for (const fgSystem of faSystems.controls) {
                const faTalkgroups = fgSystem.get('talkgroups') as FormArray;
                for (const fgTalkgroup of faTalkgroups.controls) {
                    if (fgTalkgroup.value.tagId === fgTag.value.id) {
                        if (fgTalkgroup.value.checked) on++; else off++;
                    }
                }
            }
            this.indeterminate.tags[index] = !!off && !!on;
            fgTag.get('checked')?.setValue(!off && on, { emitEvent: false });
        }
    }

    ngOnDestroy(): void {
        this.subs.unsubscribe();
    }
}
