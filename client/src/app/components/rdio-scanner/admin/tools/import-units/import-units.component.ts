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

import { Component, EventEmitter, OnInit, Output } from '@angular/core';
import { parseCsv } from '../../../../../shared/csv';
import { Config, RdioScannerAdminService, System } from '../../admin.service';

@Component({
    selector: 'rdio-scanner-admin-import-units',
    styleUrls: ['./import-units.component.scss'],
    templateUrl: './import-units.component.html',
})
export class RdioScannerAdminImportUnitsComponent implements OnInit{
    @Output() config = new EventEmitter<Config>();

    baseConfig: Config = {};

    csv: string[][] = [];

    system: System | undefined;

    tableColumns = ['id', 'label', 'action'];

    constructor(private adminService: RdioScannerAdminService) { }

    async ngOnInit(): Promise<void> {
        this.baseConfig = await this.adminService.getConfig();

        if (Array.isArray(this.baseConfig.systems) && this.baseConfig.systems.length > 0) {
            this.system = this.baseConfig.systems![0];
        }
    }

    async import(): Promise<void> {
        if (this.system === undefined) return;

        const units = this.csv.map((csv, idx) => ({
            id: +csv[0],
            label: csv[1],
            order: idx + 1,
        }));

        this.system!.units = this.system!.units!.concat(units);

        this.csv = [];

        this.config.emit(this.baseConfig);
    }

    async read(event: Event): Promise<void> {
        const target = (event.target as HTMLInputElement & EventTarget);

        const file = target.files?.item(0);

        if (!(file instanceof File)) return;

        const reader = new FileReader();

        reader.onloadend = () => {
            target.value = '';

            if (typeof reader.result !== 'string') {
                return;
            }

            this.csv = parseCsv(reader.result)
                .filter((row) => row.length > 0 && /^[0-9]+$/.test(row[0]))
                .filter((row, idx, arr) => arr.findIndex((other) => other[0] === row[0]) === idx);
        };

        reader.readAsText(file);
    }
}
