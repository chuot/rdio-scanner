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

import { DOCUMENT } from '@angular/common';
import { Component, EventEmitter, Inject, Output } from '@angular/core';
import { MatSnackBar } from '@angular/material/snack-bar';
import { Config, RdioScannerAdminService } from '../../admin.service';

@Component({
    selector: 'rdio-scanner-admin-import-export-config',
    styleUrls: ['./import-export-config.component.scss'],
    templateUrl: './import-export-config.component.html',
})
export class RdioScannerAdminImportExportConfigComponent {
    @Output() config = new EventEmitter<Config>();

    constructor(
        private adminService: RdioScannerAdminService,
        @Inject(DOCUMENT) private document: Document,
        private matSnackBar: MatSnackBar,
    ) { }

    async export(): Promise<void> {
        const config = await this.adminService.getConfig();

        if ('docker' in config) {
            delete config.docker;
        }

        const file = JSON.stringify(config);
        const fileName = 'rdio-scanner.json';
        const fileType = 'application/json';
        const fileUri = `data:${fileType};base64,${btoa(file)}`;

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
                const config = JSON.parse(reader.result as string);

                this.config.emit(config);

            } catch (error) {
                this.matSnackBar.open(error.message, '', { duration: 5000 });
            }
        };

        reader.readAsBinaryString(file);
    }
}
