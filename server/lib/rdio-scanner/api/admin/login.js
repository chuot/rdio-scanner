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

import { Log } from '../../log.js';

const MAX_ATTEMPTS = 3;
const MAX_DELAY = 10 * 60 * 1000;

const attempts = {};

export class Login {
    constructor (ctx) {
        this.path = '/api/admin/login';

        this.admin = ctx.admin;

        this.config = ctx.config;

        this.log = ctx.log;

        ctx.router.delete(this.path, this.delete());
        ctx.router.get(this.path, this.get());
        ctx.router.patch(this.path, this.patch());
        ctx.router.post(this.path, this.post());
        ctx.router.put(this.path, this.put());
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
        return (req, res) => {
            if (req.ip in attempts &&
                attempts[req.ip].count > MAX_ATTEMPTS &&
                new Date().getTime() - attempts[req.ip].date.getTime() < MAX_DELAY) {
                return res.sendStatus(401);
            }

            let token;

            if (typeof req.body.password === 'string' && req.body.password.length) {
                token = this.admin.login(req.body.password);
            }

            if (token) {
                res.send({
                    passwordNeedChange: this.config.adminPasswordNeedChange,
                    token,
                });

            } else {
                if (req.ip in attempts) {
                    attempts[req.ip].count++;
                    attempts[req.ip].date = new Date();

                } else {
                    attempts[req.ip] = {
                        count: 1,
                        date: new Date(),
                    };
                }

                if (attempts[req.ip].count === MAX_ATTEMPTS + 1) {
                    this.log.write(Log.warn, `Admin: Too many login attempts for ip ${req.ip}`);

                } else {
                    this.log.write(Log.warn, `Admin: Invalid login attempt for ip ${req.ip}`);
                }

                res.sendStatus(401);
            }

            Object.keys(attempts).forEach((ip) => {
                if (new Date().getTime() - attempts[ip].date.getTime() > MAX_DELAY) {
                    delete attempts[ip];
                }
            });
        };
    }

    put() {
        return (req, res) => {
            res.sendStatus(405);
        };
    }
}
