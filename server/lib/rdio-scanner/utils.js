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

const fs = require('fs');
const path = require('path');
const uuid = require('uuid');

class Utils {
    constructor(ctx = {}) {
        this.config = ctx.config;
    }

    loadRRDB(sysId, input) {
        if (typeof sysId !== 'number' || sysId < 0) {
            throw new Error(`Invalid system id ${JSON.stringify(sysId)}`);
        }

        if (typeof input !== 'string' || !input.length) {
            throw new Error(`Invalid filename ${JSON.stringify(input)}`);
        }

        input = path.resolve(__dirname, input);

        if (!fs.existsSync(input)) {
            throw new Error(`${input} file not found!`);
        }

        const csv = fs.readFileSync(input, 'utf8').split(/[\n|\r|\r\n]/).map((csv) => csv.split(/,/));

        const system = {
            id: sysId,
            label: path.parse(input).name,
            talkgroups: [],
        };

        for (let line of csv) {
            const encrypted = typeof line[3] === 'string' ? line[3].includes('E') : false;
            const id = parseInt(line[0], 10);
            const label = line[2] || '';
            const name = line[4] || '';
            const tag = line[5] || '';
            const group = line[6] || '';

            if (!encrypted && id > 0 && label.length && name.length && tag.length && group.length) {
                system.talkgroups.push({ id, label, name, tag, group });
            }
        }

        system.talkgroups.sort((a, b) => a.id - b.id);

        this.config.systems = this.config.systems.filter((system) => system.id !== sysId);

        this.config.systems.push(system);

        this.config.systems.sort((a, b) => a.id - b.id);

        return `File ${input} imported successfully into system ${sysId}`;
    }

    loadTR(sysId, input) {
        if (typeof sysId !== 'number' || sysId < 0) {
            throw new Error(`Invalid system id ${JSON.stringify(sysId)}`);
        }

        if (typeof input !== 'string' || !input.length) {
            throw new Error(`Invalid filename ${JSON.stringify(input)}`);
        }

        input = path.resolve(__dirname, input);

        if (!fs.existsSync(input)) {
            throw new Error(`${input} file not found!`);
        }

        const csv = fs.readFileSync(input, 'utf8').split(/[\n|\r|\r\n]/).map((csv) => csv.split(/,/));

        const system = {
            id: sysId,
            label: path.parse(input).name,
            talkgroups: [],
        };

        for (let line of csv) {
            const id = parseInt(line[0], 10);
            const label = line[3] || '';
            const name = line[4] || '';
            const tag = line[5] || '';
            const group = line[6] || '';

            if (id > 0 && label.length && name.length && tag.length && group.length) {
                system.talkgroups.push({ id, label, name, tag, group });
            }
        }

        system.talkgroups.sort((a, b) => a.id - b.id);

        this.config.systems = this.config.systems.filter((system) => system.id !== sysId);

        this.config.systems.push(system);

        this.config.systems.sort((a, b) => a.id - b.id);

        return `File ${input} imported successfully into system ${sysId}`;
    }

    randomUUID(count = 1) {
        const uuids = [];

        for (let i = 0; i < count; i++) {
            uuids.push(uuid.v4());
        }

        return uuids;
    }
}

module.exports = Utils;