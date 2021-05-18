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

'use strict';

import EventEmitter from 'events';
import Sequelize from 'sequelize';

export class Log extends EventEmitter {
    constructor(ctx) {
        super();

        this._db = (ctx.models && ctx.models.log) || null;
    }

    async read(params = {}) {
        const options = {
            order: [['dateTime', 'DESC']],
        };
        const where = {};

        params = params !== null && typeof params === 'object' ? params : {};

        if (params.on instanceof Date) {
            where.dateTime = params.on;
        }

        if (params.from instanceof Date && params.to instanceof Date) {
            where.dateTime = {
                [Sequelize.Op.gte]: params.from,
                [Sequelize.Op.lte]: params.to,
            };
        }

        if (this._assertLevel(params.level)) {
            where.level = params.level;
        }

        if (Array.isArray(params.levels)) {
            where.level = {
                [Sequelize.Op.in]: params.level.filter((level) => this._assertLevel(level)),
            };
        }

        if (typeof params.limit === 'number') {
            options.limit  = params.limit;
        }

        if (typeof params.offset === 'number') {
            options.offset = params.limit;
        }

        let logs;

        try {
            logs = await this._db.findAll({ where, options });

        } catch (error) {
            console.error(`Log: ${error.message}`);
        }

        return logs;
    }

    async write(level, message) {
        if (!this._assertLevel(level)) {
            console.error(`Log: unknown log level ${level}`);

            return;
        }

        const dateTime = new Date();

        try {
            await this._db.create({ dateTime, level, message });

            console.log(message);

        } catch (error) {
            console.error(`Log: ${error.message}`);
        }

        this.emit('log', { level, message });
    }

    _assertLevel(level) {
        return [Log.info, Log.warn, Log.error].includes(level);
    }
}

Log.info = 'info';
Log.warn = 'warn';
Log.error = 'error';
