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

const CallUpload = require('./call-upload');
const TrunkRecorderCallUpload = require('./trunk-recorder-call-upload');

class API {
    constructor(ctx = {}) {
        this.config = ctx.config;

        this.controller = ctx.controller;

        this.router = ctx.router;

        this.routes = {
            callUpload: new CallUpload(this),
            trunkRecorderCallUpload: new TrunkRecorderCallUpload(this),
        };
    }
}

module.exports = API
