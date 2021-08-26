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

import bcrypt from 'bcrypt';
import crypto from 'crypto';
import jwt from 'jsonwebtoken';
import Sequelize from 'sequelize';

import { Log } from './log.js';

export class Admin {
    constructor(ctx) {
        this.config = ctx.config;

        this.log = ctx.log;

        this.models = ctx.models;

        this.tokens = [];
    }

    changePassword(currentPassword, newPassword) {
        if (bcrypt.compareSync(currentPassword, this.config.adminPassword)) {

            this.config.adminPassword = newPassword;

            this.log.write(Log.info, 'Admin: admin password changed.');

            return true;

        } else {
            return false;
        }
    }

    async getLogs(options) {
        const filters = [];

        if (options && typeof options.level === 'string' && options.level.length) {
            filters.push({ level: options.level });
        }

        const attributes = ['_id', 'dateTime', 'level', 'message'];

        const date = options && options.date instanceof Date ? options.date : null;

        const limit = options && typeof options.limit === 'number' ? Math.min(500, options.limit) : 500;

        const offset = options && typeof options.offset === 'number' ? options.offset : 0;

        const order = [['dateTime', options && typeof options.sort === 'number' && options.sort < 0 ? 'DESC' : 'ASC']];

        const where1 = filters.length ? { [Sequelize.Op.and]: filters } : {};

        const where2 = date ? {
            [Sequelize.Op.and]: [
                where1,
                {
                    dateTime: {
                        [Sequelize.Op.gte]: new Date(date.getFullYear(), date.getMonth(), date.getDate(), 0, 0, 0),
                        [Sequelize.Op.lte]: new Date(date.getFullYear(), date.getMonth(), date.getDate(), 23, 59, 59),
                    },
                },
            ],
        } : where1;

        const [dateStartQuery, dateStopQuery, count, logs] = await Promise.all([
            await this.models.log.findOne({ attributes: ['dateTime'], order: [['dateTime', 'ASC']], where: where1 }),
            await this.models.log.findOne({ attributes: ['dateTime'], order: [['dateTime', 'DESC']], where: where1 }),
            await this.models.log.count({ where: where2 }),
            await this.models.log.findAll({ attributes, limit, offset, order, where: where2 }),
        ]);

        const dateStart = dateStartQuery && dateStartQuery.get('dateTime');

        const dateStop = dateStopQuery && dateStopQuery.get('dateTime');

        return { count, dateStart, dateStop, options, logs };
    }

    login(password) {
        if (bcrypt.compareSync(password, this.config.adminPassword)) {
            const token = jwt.sign(crypto.randomBytes(32).toString('hex'), this.config.secret);

            this.tokens.unshift(token);

            this.tokens.splice(5);

            return token;

        } else {
            return null;
        }
    }

    logout(token) {
        const index = this.tokens.findIndex((tk) => tk === token);

        if (index !== -1) {
            this.tokens.splice(index, 1);

            return true;

        } else {
            return false;
        }
    }

    validateToken(token) {
        const index = this.tokens.findIndex((tk) => tk === token);

        if (index === -1) {
            return false;
        }

        try {
            jwt.verify(token, this.config.secret);

        } catch (error) {
            this.tokens.splice(index, 1);

            return false;
        }

        return true;
    }
}
