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

import { Component, EventEmitter, Output } from '@angular/core';
import { Config, Group, RdioScannerAdminService } from '../../admin.service';

@Component({
    selector: 'rdio-scanner-admin-import-csv',
    styleUrls: ['./import-csv.component.scss'],
    templateUrl: './import-csv.component.html',
})
export class RdioScannerAdminImportCsvComponent {
    @Output() config = new EventEmitter<Config>();

    csv: string[][] = [];

    fields = [
        // [id, label, description, tag, group]
        [0, 3, 4, 5, 6], // trunk-recorder
        [0, 2, 4, 5, 6], // radioreference.com
    ];

    mode = 0;

    tableColumns = ['id', 'label', 'description', 'tag', 'group', 'action'];

    constructor(private adminService: RdioScannerAdminService) { }

    async import(): Promise<void> {
        const config = await this.adminService.getConfig();

        this.csv.forEach((tg) => {
            const group = tg[this.fields[this.mode][4]];

            if (!config.groups?.find((g) => g.label === group)) {
                const id = config.groups?.reduce((pv, cv) => typeof cv._id === 'number' && cv._id >= pv ? cv._id + 1 : pv, 0);

                config.groups?.push({ _id: id, label: group });
            }

            const tag = tg[this.fields[this.mode][3]];

            if (!config.tags?.find((t) => t.label === tag)) {
                const id = config.tags?.reduce((pv, cv) => typeof cv._id === 'number' && cv._id >= pv ? cv._id + 1 : pv, 0);

                config.tags?.push({ _id: id, label: tag });
            }
        });

        const talkgroups = this.csv.map((csv) => {
            const groupId = config.groups?.find((g) => g.label === csv[this.fields[this.mode][4]])?._id;
            const tagId = config.tags?.find((t) => t.label === csv[this.fields[this.mode][3]])?._id;

            return {
                id: +csv[this.fields[this.mode][0]],
                label: csv[this.fields[this.mode][1]],
                name: csv[this.fields[this.mode][2]],
                tagId,
                groupId,
            };
        });

        config.systems?.unshift({ talkgroups });

        this.csv = [];

        this.config.emit(config);
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

            this.csv = reader.result
                .split(/\n|\r\n/)
                .map((tg) => tg.replace(/^"|"$/g, '').split(/"*,"*/))
                .filter((tg) => tg && /^[0-9]+$/.test(tg[0]))
                .filter((tg, idx, arr) => arr.findIndex((a) => a[0] === tg[0]) === idx);
        };

        reader.readAsBinaryString(file);
    }
}
