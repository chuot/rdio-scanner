/*
 * *****************************************************************************
 * Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
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

        // Use a Blob URL so non-ASCII fields (branding text, talkgroup
        // names with accents, etc.) survive the round-trip. The previous
        // implementation percent-encoded JSON, mapped each %XX to a
        // Latin-1 char, then btoa()'d the result -- a workaround for
        // btoa()'s Latin-1-only restriction that was easy to break.
        const blob = new Blob([JSON.stringify(config, null, 2)], { type: 'application/json' });
        const url = URL.createObjectURL(blob);

        const el = this.document.createElement('a');
        el.style.display = 'none';
        el.href = url;
        el.download = 'rdio-scanner.json';

        this.document.body.appendChild(el);
        el.click();
        this.document.body.removeChild(el);

        // Free the blob URL on the next tick so the click() has had a
        // chance to register.
        setTimeout(() => URL.revokeObjectURL(url), 0);
    }

    async import(event: Event): Promise<void> {
        const target = (event.target as HTMLInputElement & EventTarget);

        const file = target.files?.item(0);

        if (!(file instanceof File)) return;

        const reader = new FileReader();

        reader.onloadend = () => {
            target.value = '';

            if (typeof reader.result !== 'string') {
                this.matSnackBar.open('Could not read the file.', '', { duration: 5000 });
                return;
            }

            // readAsBinaryString is deprecated and the previous decode
            // path mangled UTF-8. readAsText decodes UTF-8 cleanly so
            // JSON.parse just works on the result.
            try {
                const cfg = JSON.parse(reader.result) as Config;
                this.config.emit(cfg);
                this.matSnackBar.open('Configuration loaded. Save to apply.', '', { duration: 4000 });
            } catch (error) {
                this.matSnackBar.open(`Invalid config JSON: ${error instanceof Error ? error.message : String(error)}`, '', { duration: 6000 });
            }
        };

        reader.readAsText(file);
    }
}
