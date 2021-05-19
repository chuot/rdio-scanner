/*
 * *****************************************************************************
 * Copyright (C) 2019-2021 Chrystian Huot
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

'use strict';

import fs from 'fs';
import path from 'path';
import url from 'url';
import { v4 as uuidv4 } from 'uuid';

const dirname = path.dirname(url.fileURLToPath(import.meta.url));

export class Utils {
    constructor(ctx = {}) {
        this.config = ctx.config;
    }

    loadCSV(sysId, file, type) {
        if (typeof sysId !== 'number' || sysId < 0) {
            throw new Error(`Invalid system id ${JSON.stringify(sysId)}`);
        }

        if (typeof file !== 'string' || !file.length) {
            throw new Error(`Invalid filename ${JSON.stringify(file)}`);
        }

        file = path.resolve(dirname, file);

        if (!fs.existsSync(file)) {
            throw new Error(`${file} file not found!`);
        }

        type = [0, 1].includes(type) ? type : 0;

        // [id, label, description, tag, group]
        const fields = [
            [0, 3, 4, 5, 6], // trunk-recorder
            [0, 2, 4, 5, 6], // radioreference.com
        ];

        const groups = this.config.groups;

        const tags = this.config.tags;

        const csv = fs.readFileSync(file, 'utf8').split(/[\n|\r|\r\n]/).map((csv) => csv.split(/,/));

        const system = {
            id: sysId,
            label: path.parse(file).name,
            talkgroups: [],
        };

        for (let line of csv) {
            const id = parseInt(line[fields[type][0]], 10);
            const label = line[fields[type][1]] || '';
            const name = line[fields[type][2]] || '';
            const tagLabel = line[fields[type][3]] || '';
            const groupLabel = line[fields[type][4]] || '';

            if (isNaN(id) || id <= 0 || !label.length || !name.length) continue;

            if (!groups.find((g) => g.label === groupLabel)) {
                const id = groups.reduce((pv, cv) => typeof cv._id === 'number' && cv._id >= pv ? cv._id + 1 : pv, 1);
                console.log(id, groupLabel);

                groups.push({ _id: id, label: groupLabel });
            }

            if (!tags.find((t) => t.label === tagLabel)) {
                const id = tags.reduce((pv, cv) => typeof cv._id === 'number' && cv._id >= pv ? cv._id + 1 : pv, 1);

                tags.push({ _id: id, label: tagLabel });
            }

            const groupId = groups.find((g) => g.label === groupLabel)?._id;

            const tagId = tags.find((t) => t.label === tagLabel)?._id;

            system.talkgroups.push({ id, label, name, tagId, groupId });
        }

        system.talkgroups.sort((a, b) => a.label.localeCompare(b.label));

        this.config.groups = groups;
        this.config.tags = tags;
        this.config.systems = this.config.systems.filter((sys) => sys.id !== sysId).concat(system);
    }

    randomUUID(count = 1) {
        const uuids = [];

        for (let i = 0; i < count; i++) {
            uuids.push(uuidv4());
        }

        return uuids;
    }
}