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
import path from 'path';
import Umzug from 'umzug';
import url from 'url';

import { Admin } from './admin.js';
import { API } from './api.js';
import { Config } from './config.js';
import { Controller } from './controller.js';
import { defaults } from './defaults.js';
import { DirWatch } from './dir-watch.js';
import { Downstream } from './downstream.js';
import { Log } from './log.js';
import { Models } from './models.js';
import { Utils } from './utils.js';
import { WebSocket } from './websocket.js';

import rs001CreateSystem from './migrations/20191028144433-create-rdio-scanner-system.js';
import rs002CreateCall from './migrations/20191029092201-create-rdio-scanner-call.js';
import rs003OptimizeCalls from './migrations/20191126135515-optimize-rdio-scanner-calls.js';
import rs004NewV3Tables from './migrations/20191220093214-new-v3-tables.js';
import rs005OptimizeCalls from './migrations/20200123094105-optimize-rdio-scanner-calls.js';
import rs006NewV4Tables from './migrations/20200428132918-new-v4-tables.js';
import rs007NewV51Tables from './migrations/20210115105958-new-v5.1-tables.js';

const dirname = path.dirname(url.fileURLToPath(import.meta.url));

const migrations = [
    { name: '20191028144433-create-rdio-scanner-system', up: rs001CreateSystem.up, down: rs001CreateSystem.down },
    { name: '20191029092201-create-rdio-scanner-call', up: rs002CreateCall.up, down: rs002CreateCall.down },
    { name: '20191126135515-optimize-rdio-scanner-calls', up: rs003OptimizeCalls.up, down: rs003OptimizeCalls.down },
    { name: '20191220093214-new-v3-tables', up: rs004NewV3Tables.up, down: rs004NewV3Tables.down },
    { name: '20200123094105-optimize-rdio-scanner-calls', up: rs005OptimizeCalls.up, down: rs005OptimizeCalls.down },
    { name: '20200428132918-new-v4-tables', up: rs006NewV4Tables.up, down: rs006NewV4Tables.down },
    { name: '20210115105958-new-v5.1-tables', up: rs007NewV51Tables.up, down: rs007NewV51Tables.down },
];

export class RdioScanner extends EventEmitter {
    static async migrate(sequelize) {
        const umzug = new Umzug({
            migrations: Umzug.migrationsList(migrations, [
                sequelize.getQueryInterface(),
                sequelize.Sequelize,
            ]),
            storage: 'sequelize',
            storageOptions: { sequelize },
        });

        umzug.on('migrating', (migration) => process.stdout.write(`Running migration ${migration}...`));

        umzug.on('migrated', () => process.stdout.write(' done\n'));

        return umzug.up();
    }

    constructor(app = {}) {
        super();

        this.httpServer = app.httpServer;

        this.router = app.router;

        this.sequelize = app.sequelize;

        this.models = new Models(this);

        this.log = new Log(this);

        this.config = new Config(this);

        this.utils = new Utils(this);

        this.config.once('ready', async () => {
            const cmd = process.argv[2];

            if (cmd === 'load-rrdb' || cmd === 'load-tr') {
                if (process.argv.length === 5) {
                    try {
                        const sysId = parseInt(process.argv[3], 10);

                        const file = path.resolve(process.env.APP_DATA || dirname, process.argv[4]);

                        this.utils.loadCSV(sysId, file, cmd !== 'load-tr' ? 1 : 0);

                        this.config.once('persisted', () => {
                            console.log(`File ${file} imported successfully into system ${sysId}`);

                            process.exit();
                        });

                    } catch (error) {
                        console.error(error.message);

                        process.exit();
                    }

                } else {
                    console.log('USAGE: load-rrdb <system_id> <input_tg_csv>');

                    process.exit();
                }

            } else if (cmd === 'random-uuid') {
                const count = parseInt(process.argv[3], 10) || 1;

                this.utils.randomUUID(count).forEach((uuid) => console.log(uuid));

                process.exit();

            } else if (cmd === 'reset-admin-password') {
                if (typeof process.argv[3] === 'string' && process.argv[3].length) {
                    this.config.adminPassword = process.argv[3];

                    this.config.adminPasswordNeedChange = false;

                } else {
                    this.config.adminPassword = defaults.adminPassword;

                    this.config.adminPasswordNeedChange = true;
                }

                this.config.once('persisted', () => {
                    console.log('Admin password has been reset.');

                    this.log.write(Log.warn, 'CmdLine: Admin password has been reset');

                    process.exit();
                });

            } else {
                this.admin = new Admin(this);

                this.controller = new Controller(this);

                this.api = new API(this);

                this.dirWatch = new DirWatch(this);

                this.downstream = new Downstream(this);

                this.wsServer = new WebSocket(this);

                this.log.write(Log.warn, 'Server: started');

                process.nextTick(() => this.emit('ready'));
            }
        });
    }
}
