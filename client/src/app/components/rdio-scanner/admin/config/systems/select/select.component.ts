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

import { ChangeDetectorRef, Component, Inject, ViewEncapsulation } from '@angular/core';
import { FormArray, FormBuilder, FormControl, FormGroup } from '@angular/forms';
import { MAT_DIALOG_DATA, MatDialogRef } from '@angular/material/dialog';
import { Access } from '../../../admin.service';

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
})
export class RdioScannerAdminSystemsSelectComponent {
    indeterminate = {
        everything: false,
        groups: [] as boolean[],
        systems: [] as boolean[],
        tags: [] as boolean[],
    };

    select = this.ngFormBuilder.group({
        all: this.ngFormBuilder.control(false),
        groups: this.ngFormBuilder.array([]),
        tags: this.ngFormBuilder.array([]),
        systems: this.ngFormBuilder.array([]),
    });

    configTalkgroups = this.configSystems.map((fgSystem) => {
        const faTalkgroups = fgSystem.get('talkgroups') as FormArray;

        return faTalkgroups.controls as FormGroup[];
    });

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

    constructor(
        @Inject(MAT_DIALOG_DATA) public access: FormGroup,
        private matDialogRef: MatDialogRef<RdioScannerAdminSystemsSelectComponent>,
        private ngChangeDetectorRef: ChangeDetectorRef,
        private ngFormBuilder: FormBuilder,
    ) {
        const fcAll = this.select.get('all') as FormControl;
        const faGroups = this.select.get('groups') as FormArray;
        const faSystems = this.select.get('systems') as FormArray;
        const faTags = this.select.get('tags') as FormArray;

        this.configGroups.forEach((configGroup) => {
            const fgGroup = this.ngFormBuilder.group({
                _id: [configGroup.value._id],
                checked: [false],
            });

            faGroups.push(fgGroup);

            fgGroup.valueChanges.subscribe((vGroup) => {
                faSystems.controls.forEach((fgSystem) => {
                    const faTalkgroups = fgSystem.get('talkgroups') as FormArray;

                    faTalkgroups.controls.forEach((fgTalkgroup) => {
                        if (fgTalkgroup.value.groupId === vGroup._id && fgTalkgroup.value.checked !== vGroup.checked) {
                            fgTalkgroup.get('checked')?.setValue(vGroup.checked);
                        }
                    });
                });
            });
        });

        this.configSystems.forEach((configSystem, index) => {
            const fcSystemAll = this.ngFormBuilder.control(false);

            const faSystemTalkgroups = this.ngFormBuilder.array([]);

            const fgSystem = this.ngFormBuilder.group({
                all: fcSystemAll,
                id: configSystem.value.id,
                talkgroups: faSystemTalkgroups
            });

            this.configTalkgroups[index].forEach((configTalkgroup) => {
                const fgSystemTalkgroup = this.ngFormBuilder.group({
                    checked: [false],
                    groupId: configTalkgroup.value.groupId,
                    id: configTalkgroup.value.id,
                    tagId: configTalkgroup.value.tagId
                });

                faSystemTalkgroups.push(fgSystemTalkgroup);

                fgSystemTalkgroup.valueChanges.subscribe(() => {
                    const vAll = faSystemTalkgroups.controls.every((systemTalkgroup) => systemTalkgroup.value.checked);

                    fcSystemAll.setValue(vAll, { emitEvent: false });
                });
            });

            faSystems.push(fgSystem);

            fgSystem.valueChanges.subscribe(() => {
                this.rebuildGroupIndeterminates();
                this.rebuildTagIndeterminates();
            });

            fcSystemAll.valueChanges.subscribe((vAll) => {
                const faTalkgroups = fgSystem.get('talkgroups') as FormArray;

                faTalkgroups.controls.forEach((fgTalkgroup) => fgTalkgroup.get('checked')?.setValue(vAll));

                this.rebuildGroupIndeterminates();
                this.rebuildTagIndeterminates();
            });

            faSystemTalkgroups.valueChanges.subscribe((vSystemTalkgroups) => {
                let off = 0;
                let on = 0;

                vSystemTalkgroups.forEach((vSystemTalkgroup: Talkgroup) => {
                    if (vSystemTalkgroup.checked) {
                        on++;

                    } else {
                        off++;
                    }
                });

                this.indeterminate.systems[index] = !!off && !!on;

                faSystems.at(index).get('all')?.setValue(!off && on, { emitEvent: false });
            });
        });

        this.configTags.forEach((configTag) => {
            const fgTag = this.ngFormBuilder.group({
                _id: [configTag.value._id],
                checked: [false],
            });

            faTags.push(fgTag);

            fgTag.valueChanges.subscribe((vTag) => {
                faSystems.controls.forEach((fgSystem) => {
                    const faTalkgroups = fgSystem.get('talkgroups') as FormArray;

                    faTalkgroups.controls.forEach((fgTalkgroup) => {
                        if (fgTalkgroup.value.tagId === vTag._id && fgTalkgroup.value.checked !== vTag.checked) {
                            fgTalkgroup.get('checked')?.setValue(vTag.checked);
                        }
                    });
                });
            });
        });

        fcAll.valueChanges.subscribe((vAll) => {
            faSystems.controls.flatMap((fgSystem) => {
                const faTalkgroups = fgSystem.get('talkgroups') as FormArray;

                return faTalkgroups.controls;
            }).concat(faGroups.controls, faTags.controls).forEach((control) => {
                control.get('checked')?.setValue(vAll);
            });
        });

        faSystems.valueChanges.subscribe((vSystems) => {
            let off = 0;
            let on = 0;

            vSystems.forEach((vSystem: System) => {
                if (vSystem.all) {
                    on++;

                } else {
                    off++;
                }
            });

            this.indeterminate.everything = !!off && !!on;

            fcAll.setValue(!off && on, { emitEvent: false });
        });

        const vAccess: Access = this.access.value;

        if (vAccess.systems === '*') {
            this.select.get('all')?.setValue(true);

        } else if (Array.isArray(vAccess.systems)) {
            vAccess.systems.forEach((vSystem: { id: number; talkgroups: { id: number }[] | number[] | '*' } | number) => {
                if (typeof vSystem === 'number') {
                    faSystems.controls.find((fgSystem) => fgSystem.value.id === vSystem)?.get('all')?.setValue(true);

                } else if (vSystem !== null && typeof vSystem === 'object') {
                    const fgSystem = faSystems.controls.find((fg) => fg.value.id === vSystem.id);

                    if (fgSystem) {
                        if (vSystem.talkgroups === '*') {
                            fgSystem.get('all')?.setValue(true);

                        } else if (Array.isArray(vSystem.talkgroups)) {
                            const faTalkgroups = fgSystem.get('talkgroups') as FormArray;

                            vSystem.talkgroups.forEach((talkgroup: { id: number } | number) => {
                                const talkgroupId = typeof talkgroup === 'number' ? talkgroup : talkgroup.id;

                                const fgTalkgroup = faTalkgroups.controls.find((fg) => fg.value.id === talkgroupId);

                                fgTalkgroup?.get('checked')?.setValue(true);
                            });

                            fgSystem?.updateValueAndValidity();
                        }
                    }
                }
            });
        }
    }

    accept(): void {
        const select = this.select.value;

        const access: Access['systems'] = select.all ? '*' : select.systems.filter((system: System) => {
            return system.all || system.talkgroups.some((talkgroup: Talkgroup) => talkgroup.checked);
        }).map((system: System) => {
            if (system.all) {
                return {
                    id: system.id,
                    talkgroups: '*',
                };

            } else {
                return {
                    id: system.id,
                    talkgroups: system.talkgroups
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

        faGroups.controls.forEach((fgGroup, index) => {
            let off = 0;
            let on = 0;

            faSystems.controls.forEach((fgSystem) => {
                const faTalkgroups = fgSystem.get('talkgroups') as FormArray;

                faTalkgroups.controls.forEach((fgTalkgroup) => {
                    if (fgTalkgroup.value.groupId === fgGroup.value._id) {
                        if (fgTalkgroup.value.checked) {
                            on++;

                        } else {
                            off++;
                        }
                    }
                });
            });

            this.indeterminate.groups[index] = !!off && !!on;

            fgGroup.get('checked')?.setValue(!off && on, { emitEvent: false });
        });
    }

    private rebuildTagIndeterminates(): void {
        const faTags = this.select.get('tags') as FormArray;

        const faSystems = this.select.get('systems') as FormArray;

        faTags.controls.forEach((fgTag, index) => {
            let off = 0;
            let on = 0;

            faSystems.controls.forEach((fgSystem) => {
                const faTalkgroups = fgSystem.get('talkgroups') as FormArray;

                faTalkgroups.controls.forEach((fgTalkgroup) => {
                    if (fgTalkgroup.value.tagId === fgTag.value._id) {
                        if (fgTalkgroup.value.checked) {
                            on++;

                        } else {
                            off++;
                        }
                    }
                });
            });

            this.indeterminate.tags[index] = !!off && !!on;

            fgTag.get('checked')?.setValue(!off && on, { emitEvent: false });
        });
    }
}
