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

require('dotenv').config();

const cors = require('cors');
const express = require('express');
const fs = require('fs');
const helmet = require('helmet');
const http = require('http');
const https = require('https');
const path = require('path');
const Sequelize = require('sequelize');

const RdioScanner = require('./lib/rdio-scanner');

class App {
    static Config() {
        const configFile = path.resolve(process.env.APP_DATA || __dirname, 'config.json');

        let config;

        if (fs.existsSync(configFile)) {
            config = JSON.parse(fs.readFileSync(configFile));

        } else {
            config = {};
        }

        if (config.nodejs === null || typeof config.nodejs !== 'object') {
            config.nodejs = {};
        }

        config.nodejs.env = config.nodejs.env || 'production';
        config.nodejs.host = config.nodejs.host || '0.0.0.0';
        config.nodejs.port = config.nodejs.port || process.env.APP_PORT || 3000;
        config.nodejs.sslCA = config.nodejs.sslCA || null;

        if (typeof config.nodejs.sslCA === 'string') {
            config.nodejs.sslCA = path.resolve(process.env.APP_DATA || __dirname, config.nodejs.sslCA);
        }

        config.nodejs.sslCert = config.nodejs.sslCert || null;

        if (typeof config.nodejs.sslCert === 'string') {
            config.nodejs.sslCert = path.resolve(process.env.APP_DATA || __dirname, config.nodejs.sslCert);
        }

        config.nodejs.sslKey = config.nodejs.sslKey || null;

        if (typeof config.nodejs.sslKey === 'string') {
            config.nodejs.sslKey = path.resolve(process.env.APP_DATA || __dirname, config.nodejs.sslKey);
        }

        if (config.sequelize === null || typeof config.sequelize !== 'object') {
            config.sequelize = {};
        }

        config.sequelize.database = config.sequelize.database || process.env.DB_NAME || null;
        config.sequelize.dialect = config.sequelize.dialect || process.env.DB_DIALECT || 'sqlite';

        if (typeof config.sequelize.dialectOptions !== 'undefined') {
            delete config.sequelize.dialectOptions;
        }

        if (typeof config.sequelize.logging !== 'undefined') {
            delete config.sequelize.logging;
        }

        config.sequelize.host = config.sequelize.host || process.env.DB_HOST || null;
        config.sequelize.password = config.sequelize.password || process.env.DB_PASS || null;
        config.sequelize.port = config.sequelize.port || process.env.DB_PORT || null;

        if (config.sequelize.dialect === 'sqlite') {
            config.sequelize.storage = config.sequelize.storage
                ? path.resolve(__dirname, config.sequelize.storage)
                : path.resolve(process.env.APP_DATA || process.env.DB_STORAGE || __dirname, 'database.sqlite');

        } else {
            config.sequelize.storage = null;
        }

        config.sequelize.username = config.sequelize.username || process.env.DB_USER || null;

        config.persist = () => {
            const sortProperties = (obj) => Object.keys(obj)
                .filter((key) => key !== 'persist')
                .sort()
                .reduce((newObj, key) => {
                    if (!Array.isArray(obj[key]) && obj[key] !== null && typeof obj[key] === 'object') {
                        newObj[key] = sortProperties(obj[key]);

                    } else if (Array.isArray(obj[key])) {
                        newObj[key] = obj[key].map((val) => (val !== null && typeof val === 'object') ? sortProperties(val) : val);

                    } else {
                        newObj[key] = obj[key];
                    }

                    return newObj;
                }, {});

            const data = sortProperties(config);

            try {
                fs.writeFileSync(configFile, JSON.stringify(data, null, 2));

            } catch (error) {
                console.error(`Unable to persist configuration: ${error.message}`);
            }
        }

        return config;
    }

    constructor() {
        const staticFile = 'index.html';

        const staticDir = fs.existsSync(path.resolve(__dirname, `../client/${staticFile}`))
            ? path.resolve(__dirname, '../client')
            : path.resolve(__dirname, '../client/dist/rdio-scanner');

        this.config = App.Config();

        this.router = express();
        this.router.use(cors());
        this.router.use(express.json());
        this.router.use(express.urlencoded({ extended: false }));
        this.router.use(express.static(staticDir));
        this.router.set(this.config.nodejs.port);

        if (this.config.nodejs.env !== 'development') {
            this.router.disable('x-powered-by');

            this.router.get(/^(\/|\/index.html)$/, (_, res) => {
                if (fs.existsSync(path.join(staticDir, staticFile))) {
                    return res.sendFile(staticFile, { root: staticDir });

                } else {
                    return res.send('A new build is being prepared. Please check back in a few minutes.');
                }
            });

            this.router.use(helmet());
        }

        let sslMode;

        if (fs.existsSync(this.config.nodejs.sslCert) && fs.existsSync(this.config.nodejs.sslKey)) {
            sslMode = true;

            const options = {
                cert: fs.readFileSync(this.config.nodejs.sslCert),
                key: fs.readFileSync(this.config.nodejs.sslKey),
            };

            if (fs.existsSync(this.config.nodejs.sslCA)) {
                options.ca = fs.readFileSync(this.config.nodejs.sslCA);
            }

            this.httpServer = https.createServer(options, this.router);

        } else {
            sslMode = false;

            this.httpServer = http.createServer(this.router);
        }

        this.sequelize = new Sequelize(Object.assign({}, {
            dialectOptions: {
                timezone: "Etc/GMT0",
            },
            logging: false,
        }, this.config.sequelize));

        this.rdioScanner = new RdioScanner(this);

        const cmd = process.argv[2];

        if (cmd === 'init') {
            this.rdioScanner.once('ready', () => {
                this.config.persist();

                console.log('Configuration and database initialized');

                process.exit();
            });

        } else if (cmd === 'load-rrdb') {
            this.rdioScanner.once('ready', () => {
                if (process.argv.length === 5) {
                    try {
                        const sysId = parseInt(process.argv[3], 10);

                        const input = path.resolve(process.env.APP_DATA || __dirname, process.argv[4]);

                        const msg = this.rdioScanner.utils.loadRRDB(sysId, input);

                        console.log(msg);

                    } catch (error) {
                        console.error(error.message);
                    }

                } else {
                    console.log(`USAGE: ${cmd} <system_id> <input_tg_csv>`);
                }

                this.config.persist();

                process.exit();
            });

        } else if (cmd === 'load-tr') {
            this.rdioScanner.once('ready', () => {
                if (process.argv.length === 5) {
                    try {
                        const sysId = parseInt(process.argv[3], 10);

                        const input = path.resolve(process.env.APP_DATA || __dirname, process.argv[4]);

                        const msg = this.rdioScanner.utils.loadTR(sysId, input);

                        console.log(msg);

                    } catch (error) {
                        console.error(error.message);
                    }

                } else {
                    console.log(`USAGE: ${cmd} <system_id> <input_tg_csv>`);
                }

                this.config.persist();

                process.exit();
            });

        } else if (cmd === 'random-uuid') {
            this.rdioScanner.once('ready', () => {
                const count = parseInt(process.argv[3], 10) || 1;

                this.rdioScanner.utils.randomUUID(count).forEach((uuid) => console.log(uuid));

                process.exit();
            });

        } else {
            this.config.persist();

            this.httpServer.listen(this.config.nodejs.port, this.config.nodejs.host, () => {
                console.log(`Server is running at ${sslMode ? 'https' : 'http'}://${this.config.nodejs.host}:${this.config.nodejs.port}`);
            });
        }
    }
}

if (require.main === module) {
    new App();
}

module.exports = App;
