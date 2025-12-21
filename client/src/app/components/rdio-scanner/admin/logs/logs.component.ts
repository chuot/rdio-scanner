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

import { Component, ViewChild } from '@angular/core';
import { FormBuilder, FormGroup } from '@angular/forms';
import { MatPaginator } from '@angular/material/paginator';
import { BehaviorSubject } from 'rxjs';
import { Log, LogsQuery, LogsQueryOptions, RdioScannerAdminService } from '../admin.service';

@Component({
    selector: 'rdio-scanner-admin-logs',
    styleUrls: ['./logs.component.scss'],
    templateUrl: './logs.component.html',
})
export class RdioScannerAdminLogsComponent {
    form: FormGroup;

    logs = new BehaviorSubject(new Array<Log | null>(10));

    logsQuery: LogsQuery | undefined = undefined;

    logsQueryPending = false;

    private limit = 200;

    private offset = 0;

    @ViewChild(MatPaginator) private paginator: MatPaginator | undefined;

    constructor(private adminService: RdioScannerAdminService, private ngFormBuilder: FormBuilder) {
        this.form = this.ngFormBuilder.group({
            date: [null],
            level: [null],
            sort: [-1],
        });
    }

    formHandler(): void {
        this.paginator?.firstPage();

        this.reload();
    }

    reset(): void {
        this.form.reset({
            date: null,
            level: null,
            sort: -1,
        });

        this.formHandler();
    }

    refresh(): void {
        if (!this.paginator) {
            return;
        }

        const from = this.paginator.pageIndex * this.paginator.pageSize;

        const to = this.paginator.pageIndex * this.paginator.pageSize + this.paginator.pageSize - 1;

        if (!this.logsQueryPending && (from >= this.offset + this.limit || from < this.offset)) {
            this.reload();

        } else if (this.logsQuery) {
            const logs: Array<Log | null> = this.logsQuery.logs.slice(from % this.limit, to % this.limit + 1);

            while (logs.length < this.logs.value.length) {
                logs.push(null);
            }

            this.logs.next(logs);
        }
    }

    async reload(): Promise<void> {
        const pageIndex = this.paginator?.pageIndex || 0;

        const pageSize = this.paginator?.pageSize || 0;

        this.offset = Math.floor((pageIndex * pageSize) / this.limit) * this.limit;

        const options: LogsQueryOptions = {
            limit: this.limit,
            offset: this.offset,
            sort: this.form.get('sort')?.value ?? -1,
        };

        if (typeof this.form.get('level')?.value === 'string') {
            options.level = this.form.get('level')?.value ?? undefined;
        }

        if (typeof this.form.value.date === 'string') {
            options.date = new Date(Date.parse(this.form.value.date));
        }

        this.logsQueryPending = true;

        this.form.disable();

        this.logsQuery = await this.adminService.getLogs(options);

        this.form.enable();

        this.logsQueryPending = false;

        this.refresh();
    }
}
