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

export class Logs {
    constructor (ctx) {
        this.path = '/api/admin/logs';

        this.admin = ctx.admin;

        ctx.router.delete(this.path, ctx.auth(), this.delete());
        ctx.router.get(this.path, ctx.auth(), this.get());
        ctx.router.patch(this.path, ctx.auth(), this.patch());
        ctx.router.post(this.path, ctx.auth(), this.post());
        ctx.router.put(this.path, ctx.auth(), this.put());
    }

    delete() {
        return (req, res) => {
            res.sendStatus(405);
        };
    }

    get() {
        return (req, res) => {
            res.sendStatus(405);
        };
    }

    patch() {
        return (req, res) => {
            res.sendStatus(405);
        };
    }

    post() {
        return async (req, res) => {
            if (!req.auth.valid) {
                return res.sendStatus(401);
            }

            const options = {};

            if (typeof req.body.level === 'string' && req.body.level.length) {
                options.level = req.body.level;
            }

            if (typeof req.body.date === 'string' && req.body.date.length) {
                options.date = new Date(req.body.date);
            }

            if (typeof req.body.limit === 'number') {
                options.limit = req.body.limit;
            }

            if (typeof req.body.offset === 'number') {
                options.offset = req.body.offset;
            }

            if (typeof req.body.sort === 'number') {
                options.sort = req.body.sort;
            }

            const logs = await this.admin.getLogs(options);

            res.send(logs);
        };
    }

    put() {
        return (req, res) => {
            res.sendStatus(405);
        };
    }
}
