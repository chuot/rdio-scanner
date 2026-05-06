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

import { Component } from '@angular/core';
import { MatSnackBar } from '@angular/material/snack-bar';
import { RdioScannerAdminService } from '../../admin.service';

@Component({
    selector: 'rdio-scanner-admin-database',
    templateUrl: './database.component.html',
})
export class RdioScannerAdminDatabaseComponent {
    busy = false;

    pruneDays: number | null = null;

    constructor(
        private adminService: RdioScannerAdminService,
        private matSnackBar: MatSnackBar,
    ) { }

    async compact(): Promise<void> {
        if (this.busy) return;
        if (!confirm('Reclaim disk space by compacting the database now? This may take a while on large installations.')) return;

        this.busy = true;
        try {
            const res = await this.adminService.compactDatabase();
            this.matSnackBar.open(`Database compacted in ${res.durationMs} ms (${res.engine}).`, '', { duration: 5000 });
        } catch (err: unknown) {
            this.matSnackBar.open(`Compact failed: ${this.errMessage(err)}`, '', { duration: 7000 });
        } finally {
            this.busy = false;
        }
    }

    async prune(): Promise<void> {
        if (this.busy) return;

        const days = this.pruneDays;
        if (days !== null && (!Number.isFinite(days) || days < 0)) {
            this.matSnackBar.open('Days must be a non-negative number.', '', { duration: 4000 });
            return;
        }

        const message = days === null
            ? 'Prune calls older than the configured retention window?'
            : `Permanently delete calls older than ${days} day(s)?`;
        if (!confirm(message)) return;

        this.busy = true;
        try {
            const res = await this.adminService.pruneDatabase(days ?? undefined);
            this.matSnackBar.open(`Pruned calls older than ${res.days} day(s).`, '', { duration: 5000 });
        } catch (err: unknown) {
            this.matSnackBar.open(`Prune failed: ${this.errMessage(err)}`, '', { duration: 7000 });
        } finally {
            this.busy = false;
        }
    }

    private errMessage(err: unknown): string {
        if (err && typeof err === 'object') {
            const e = err as { error?: { error?: string }; message?: string; statusText?: string };
            if (e.error?.error) return e.error.error;
            if (e.message) return e.message;
            if (e.statusText) return e.statusText;
        }
        return 'unknown error';
    }
}
