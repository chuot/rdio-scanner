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

import { Config } from './api/admin/config.js';
import { Login } from './api/admin/login.js';
import { Logout } from './api/admin/logout.js';
import { Logs } from './api/admin/logs.js';
import { Password } from './api/admin/password.js';
import { UploadCall } from './api/upload/upload-call.js';
import { UploadTrunkRecorder } from './api/upload/upload-trunk-recorder.js';

export class API {
    constructor(ctx) {
        this.admin = ctx.admin;

        this.config = ctx.config;

        this.controller = ctx.controller;

        this.log = ctx.log;

        this.httpServer = ctx.httpServer;

        this.router = ctx.router;

        this.routes = {
            config: new Config(this),
            login: new Login(this),
            logout: new Logout(this),
            logs: new Logs(this),
            password: new Password(this),
            uploadCall: new UploadCall(this),
            uploadTrunkRecorder: new UploadTrunkRecorder(this),
        };
    }

    auth() {
        return (req, res, next) => {
            const token = req.get('Authorization');

            req.auth = {
                token,
                valid: this.admin.validateToken(token),
            };

            next();
        };
    }
}
