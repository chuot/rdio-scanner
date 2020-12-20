/*
 * *****************************************************************************
 * Copyright (C) 2019-2020 Chrystian Huot
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

const EventEmitter = require('events');
const http = require('http');
const https = require('https');
const Sequelize = require('sequelize');
const Umzug = require('umzug');

const API = require('./api');
const Controller = require('./controller');
const DirWatch = require('./dir-watch');
const Downstream = require('./downstream');
const Models = require('./models');
const Utils = require('./utils');
const WebSocket = require('./websocket');

const rs001CreateSystem = require('./migrations/20191028144433-create-rdio-scanner-system');
const rs002CreateCall = require('./migrations/20191029092201-create-rdio-scanner-call');
const rs003OptimizeCalls = require('./migrations/20191126135515-optimize-rdio-scanner-calls');
const rs004NewV3Tables = require('./migrations/20191220093214-new-v3-tables');
const rs005OptimizeCalls = require('./migrations/20200123094105-optimize-rdio-scanner-calls');
const rs006NewV4Tables = require('./migrations/20200428132918-new-v4-tables');
const rs007AddAudioDurationCol = require('./migrations/20201228032605-add-duration');

const migrations = [
    { name: '20191028144433-create-rdio-scanner-system', up: rs001CreateSystem.up, down: rs001CreateSystem.down },
    { name: '20191029092201-create-rdio-scanner-call', up: rs002CreateCall.up, down: rs002CreateCall.down },
    { name: '20191126135515-optimize-rdio-scanner-calls', up: rs003OptimizeCalls.up, down: rs003OptimizeCalls.down },
    { name: '20191220093214-new-v3-tables', up: rs004NewV3Tables.up, down: rs004NewV3Tables.down },
    { name: '20200123094105-optimize-rdio-scanner-calls', up: rs005OptimizeCalls.up, down: rs005OptimizeCalls.down },
    { name: '20200428132918-new-v4-tables', up: rs006NewV4Tables.up, down: rs006NewV4Tables.down },
    { name: '20201228032605-add-duration', up: rs007AddAudioDurationCol.up, down: rs007AddAudioDurationCol.down },
];

class RdioScanner extends EventEmitter {
    constructor(app = {}) {
        super();

        if (!(app.httpServer instanceof http.Server || app.httpServer instanceof https.Server)) {
            throw new Error('app.httpServer must be an instance of http.Server');
        }

        this.httpServer = app.httpServer;

        if (typeof app.router !== 'function') {
            throw new Error('app.router must be an instance of express.Router');
        }

        this.router = app.router;

        if (!(app.sequelize instanceof Sequelize)) {
            throw new Error('app.sequelize must be an instance of Sequelize');
        }

        this.sequelize = app.sequelize;

        if (app.config.rdioScanner === null || typeof app.config.rdioScanner !== 'object') {
            app.config.rdioScanner = {};
        }

        this.config = app.config.rdioScanner;

        this.models = new Models(this);

        this.controller = new Controller(this);

        this.api = new API(this);

        this.dirWatch = new DirWatch(this);

        this.downstream = new Downstream(this);

        this.utils = new Utils(this);

        this.wsServer = new WebSocket(this);

        (async () => {
            await this.migrate();

            this.emit('ready');
        })();
    }

    async migrate() {
        const umzug = new Umzug({
            migrations: Umzug.migrationsList(migrations, [
                this.sequelize.getQueryInterface(),
                this.sequelize.Sequelize,
            ]),
            storage: 'sequelize',
            storageOptions: { sequelize: this.sequelize },
        });

        return umzug.up();
    }
}

module.exports = RdioScanner;
