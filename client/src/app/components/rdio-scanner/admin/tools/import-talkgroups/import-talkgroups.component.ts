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

import { Component, EventEmitter, Output } from '@angular/core';
import { MatSnackBar } from '@angular/material/snack-bar';
import { parseCsv } from '../../../../../shared/csv';
import {
    Config,
    RadioReferenceImportRequest,
    RadioReferenceTalkgroup,
    RdioScannerAdminService,
} from '../../admin.service';

@Component({
    selector: 'rdio-scanner-admin-import-talkgroups',
    styleUrls: ['./import-talkgroups.component.scss'],
    templateUrl: './import-talkgroups.component.html',
})
export class RdioScannerAdminImportTalkgroupsComponent {
    @Output() config = new EventEmitter<Config>();

    csv: string[][] = [];

    // Index of the synthetic "kHz from frequency" cell appended to each row
    // when the upload looks like a RadioReference frequency export. Kept
    // out of the way of anything a real CSV would put there.
    private static readonly SYNTH_FREQ_KHZ_COL = 32;

    fields = [
        // [id, label, description, tag, group]
        [0, 3, 4, 5, 6], // 0: trunk-recorder
        [0, 2, 4, 5, 6], // 1: radioreference.com talkgroup CSV
        // 2: radioreference.com "Frequencies" CSV. id is synthesized from
        // the decimal-MHz "Frequency Output" column (kHz, integer). Other
        // columns: 0:Freq Out, 1:Freq In, 2:Callsign, 3:Agency/Category,
        // 4:Description, 5:Alpha Tag, 6:PL Out, 7:PL In, 8:Mode,
        // 9:Class Station Code, 10:Tag.
        [RdioScannerAdminImportTalkgroupsComponent.SYNTH_FREQ_KHZ_COL, 5, 4, 10, 3],
    ];

    mode = 0;

    tableColumns = ['id', 'label', 'description', 'tag', 'group', 'action'];

    rrForm: RadioReferenceImportRequest = {
        username: '',
        password: '',
        appKey: '',
        sid: 0,
    };

    rrFetching = false;

    constructor(
        private adminService: RdioScannerAdminService,
        private matSnackBar: MatSnackBar,
    ) { }

    async fetchFromRadioReference(): Promise<void> {
        const username = (this.rrForm.username || '').trim();
        const password = this.rrForm.password || '';
        const appKey = (this.rrForm.appKey || '').trim();
        const sid = Number(this.rrForm.sid);

        if (!username || !password || !appKey || !Number.isFinite(sid) || sid <= 0) {
            this.matSnackBar.open('Username, password, app key and a numeric system id are required.', '', { duration: 5000 });
            return;
        }

        this.rrFetching = true;

        try {
            const talkgroups = await this.adminService.importRadioReferenceTalkgroups({
                username, password, appKey, sid,
            });

            if (!talkgroups.length) {
                this.matSnackBar.open('No talkgroups returned for that system.', '', { duration: 5000 });
                return;
            }

            // Map RR records into the radioreference.com CSV column layout
            // expected by mode=1: [DEC, HEX, AlphaTag, Mode, Description, Tag, Category]
            this.csv = talkgroups.map((tg: RadioReferenceTalkgroup) => [
                String(tg.id ?? ''),
                tg.hex || '',
                tg.alphaTag || '',
                tg.mode || '',
                tg.description || '',
                tg.tag || '',
                tg.group || '',
            ]).filter((row) => /^[0-9]+$/.test(row[0]));

            this.mode = 1;

            this.matSnackBar.open(`Loaded ${this.csv.length} talkgroups from Radio Reference.`, '', { duration: 4000 });

        } catch (err: unknown) {
            const message = this.extractError(err);
            this.matSnackBar.open(`Radio Reference import failed: ${message}`, '', { duration: 7000 });

        } finally {
            this.rrFetching = false;
        }
    }

    async import(): Promise<void> {
        const config = await this.adminService.getConfig();

        this.csv.forEach((tg) => {
            const group = tg[this.fields[this.mode][4]];

            if (!config.groups?.find((g) => g.label === group)) {
                const id = config.groups?.reduce((pv, cv) => typeof cv._id === 'number' && cv._id >= pv ? cv._id + 1 : pv, 1);

                config.groups?.push({ _id: id, label: group });
            }

            const tag = tg[this.fields[this.mode][3]];

            if (!config.tags?.find((t) => t.label === tag)) {
                const id = config.tags?.reduce((pv, cv) => typeof cv._id === 'number' && cv._id >= pv ? cv._id + 1 : pv, 1);

                config.tags?.push({ _id: id, label: tag });
            }
        });

        const talkgroups = this.csv.map((csv, idx) => {
            const groupId = config.groups?.find((g) => g.label === csv[this.fields[this.mode][4]])?._id;
            const tagId = config.tags?.find((t) => t.label === csv[this.fields[this.mode][3]])?._id;

            return {
                id: +csv[this.fields[this.mode][0]],
                label: csv[this.fields[this.mode][1]],
                name: csv[this.fields[this.mode][2]],
                order: idx + 1,
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

            const synthCol = RdioScannerAdminImportTalkgroupsComponent.SYNTH_FREQ_KHZ_COL;

            const rows = parseCsv(reader.result)
                .map((row) => row.map((cell) => (typeof cell === 'string' ? cell.trim() : cell)));

            // Two accepted shapes:
            //   1. Talkgroup CSV (trunk-recorder / RR talkgroups): col 0 is a
            //      pure integer talkgroup id.
            //   2. RR Frequencies CSV: col 0 is a decimal MHz value; we
            //      synthesize an integer kHz id and stash it in synthCol so
            //      modes 0/1 still read row[0] and mode 2 reads row[synthCol].
            // A row that doesn't fit either shape is dropped.
            let freqRowCount = 0;
            let talkgroupRowCount = 0;

            const enriched = rows.map((row) => {
                const out = row.slice();
                const first = out[0] ?? '';

                if (/^\d+$/.test(first)) {
                    talkgroupRowCount++;
                } else if (/^\d+(\.\d+)?$/.test(first) && parseFloat(first) > 0) {
                    // Round to nearest kHz, away from 0; covers both
                    // 147.000000 (MHz) and 462.5500 cleanly without
                    // precision drift.
                    out[synthCol] = String(Math.round(parseFloat(first) * 1000));
                    freqRowCount++;
                }

                return out;
            });

            const accepted = enriched
                .filter((row) => /^\d+$/.test(row[0] ?? '') || /^\d+$/.test(row[synthCol] ?? ''))
                .filter((row, idx, arr) => {
                    const idOf = (r: string[]): string => (/^\d+$/.test(r[0] ?? '') ? r[0] : r[synthCol]) || '';
                    const myId = idOf(row);
                    return arr.findIndex((other) => idOf(other) === myId) === idx;
                });

            // Auto-pick mode 2 if the CSV is clearly a frequency export.
            // Don't override an existing selection if both shapes are mixed.
            if (freqRowCount > talkgroupRowCount && freqRowCount > 0) {
                this.mode = 2;
            } else if (this.mode === 2 && talkgroupRowCount > freqRowCount) {
                this.mode = 0;
            }

            this.csv = accepted;

            if (accepted.length === 0) {
                this.matSnackBar.open(
                    `No usable rows in that CSV. ${rows.length} non-empty lines parsed; expected either a numeric talkgroup id or a decimal-MHz frequency in column 1.`,
                    '',
                    { duration: 7000 },
                );
            } else {
                const shape = this.mode === 2 ? 'frequency' : 'talkgroup';
                this.matSnackBar.open(
                    `Loaded ${accepted.length} ${shape} row(s) from CSV.`,
                    '',
                    { duration: 4000 },
                );
            }
        };

        reader.readAsText(file);
    }

    private extractError(err: unknown): string {
        if (err && typeof err === 'object') {
            const e = err as { error?: { error?: string }; message?: string; statusText?: string };
            if (e.error?.error) return e.error.error;
            if (e.message) return e.message;
            if (e.statusText) return e.statusText;
        }
        return 'unknown error';
    }
}

