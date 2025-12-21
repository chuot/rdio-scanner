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

import { DOCUMENT } from '@angular/common';
import { Component, EventEmitter, Inject, Output } from '@angular/core';
import { MatSnackBar } from '@angular/material/snack-bar';
import { Config, RdioScannerAdminService } from '../../admin.service';
import packageInfo from '../../../../../../../package.json';

@Component({
    selector: 'rdio-scanner-admin-import-export-config',
    styleUrls: ['./import-export-config.component.scss'],
    templateUrl: './import-export-config.component.html',
})
export class RdioScannerAdminImportExportConfigComponent {
    @Output() config = new EventEmitter<Config>();

    version = packageInfo.version;

    constructor(
        private adminService: RdioScannerAdminService,
        @Inject(DOCUMENT) private document: Document,
        private matSnackBar: MatSnackBar,
    ) { }

    async export(): Promise<void> {
        const config = await this.adminService.getConfig();

        const file = encodeURIComponent(JSON.stringify(config)).replace(/%([0-9A-F]{2})/g, (_, c) => {
            return String.fromCharCode(parseInt(c, 16));
        });
        const fileName = `rdio-scanner-${this.version}.config.json`;
        const fileType = 'application/json';
        const fileUri = `data:${fileType};base64,${window.btoa(file)}`;

        const el = this.document.createElement('a');

        el.style.display = 'none';

        el.setAttribute('href', fileUri);
        el.setAttribute('download', fileName);

        this.document.body.appendChild(el);

        el.click();

        this.document.body.removeChild(el);
    }

    async import(event: Event): Promise<void> {
        const target = (event.target as HTMLInputElement & EventTarget);

        const file = target.files?.item(0);

        if (!(file instanceof File)) return;

        const reader = new FileReader();

        reader.onloadend = () => {
            target.value = '';

            try {
                const res = decodeURIComponent(Array.prototype.map.call(reader.result, (c) => {
                    return '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2)
                }).join(''));

                const config = JSON.parse(res);

                if (Array.isArray(config.access))
                    config.access.forEach((access: { [key: string]: unknown }) => {
                        access['id'] = access['_id'];
                    });

                config['apikeys'] = config['apiKeys'];
                if (Array.isArray(config.apikeys))
                    config.access.forEach((access: { [key: string]: unknown }) => {
                        access['id'] = access['_id'];
                    });

                config['dirwatch'] = config['dirWatch'];
                if (Array.isArray(config.dirwatch))
                    config.dirwatch.forEach((dirwatch: { [key: string]: unknown }) => {
                        dirwatch['id'] = dirwatch['_id'];
                    });

                if (Array.isArray(config.downstreams))
                    config.downstreams.forEach((downstream: { [key: string]: unknown }) => {
                        downstream['id'] = downstream['_id'];
                        downstream['apikey'] = downstream['apiKey'];
                    });

                if (Array.isArray(config.groups))
                    config.groups.forEach((group: { [key: string]: unknown }) => {
                        group['id'] = group['_id'];
                    });

                    config.groups = (config.groups as { label: string }[]).sort((a, b) => a.label.localeCompare(b.label));

                if (Array.isArray(config.tags))
                    config.tags.forEach((tag: { [key: string]: unknown }) => {
                        tag['id'] = tag['_id'];
                    });

                    config.tags = (config.tags as { label: string }[]).sort((a, b) => a.label.localeCompare(b.label));

                if (Array.isArray(config.systems))
                    config.systems.forEach((system: { [key:string]:unknown}) => {
                        system['systemRef'] = system['id'];
                        system['id'] = system['_id'];

                        const talkgroups = system['talkgroups'];

                        if (Array.isArray(talkgroups))
                            talkgroups.forEach((talkgroup: {[key:string]: unknown}) => {
                                const groupId = talkgroup['groupId'];

                                if (typeof groupId === 'number') talkgroup['groupIds'] = [groupId];

                                talkgroup['talkgroupRef'] = talkgroup['id'];
                                delete talkgroup['id'];
                            });

                    });

                this.config.emit(config);

                // const versionMajor = this.version.split('.')[0];

                // if (config['version']?.split('.')[0] === versionMajor) {
                //     this.config.emit(config);

                // } else {
                //     this.matSnackBar.open('Config file version mismatch', '', { duration: 5000 });
                // }


            } catch (error) {
                this.matSnackBar.open(error as string, '', { duration: 5000 });
            }
        };

        reader.readAsBinaryString(file);
    }
}
