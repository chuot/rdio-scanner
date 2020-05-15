/*
 * *****************************************************************************
 *  Copyright (C) 2019-2020 Chrystian Huot
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

require('dotenv').config();

const http = require('http');
const Sequelize = require('sequelize');

const Controller = require('./controller');
const Config = require('./config');
const DirWatch = require('./dir-watch');
const Downstream = require('./downstream');
const Models = require('./models');
const Routes = require('./routes');
const WebSocket = require('./websocket');

class RdioScanner {
    constructor(app = {}) {
        if (!(app.sequelize instanceof Sequelize)) {
            throw new Error('app.sequelize must be an instance of Sequelize');
        }

        this.sequelize = app.sequelize;

        if (!(app.httpServer instanceof http.Server)) {
            throw new Error('app.httpServer must be an instance of http.Server');
        }

        this.httpServer = app.httpServer;

        if (typeof app.router !== 'function') {
            throw new Error('app.router must be an instance of express.Router');
        }

        this.router = app.router;

        this.config = new Config(app.config && app.config.rdioScanner);

        this.controller = new Controller(this);

        this.dirWatch = new DirWatch(this);

        this.downstream = new Downstream(this);

        this.models = new Models(this);

        this.routes = new Routes(this);

        this.wsServer =  new WebSocket(this);
    }
}

module.exports = RdioScanner;
