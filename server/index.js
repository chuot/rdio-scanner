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
        const configFile = path.resolve(__dirname, 'config.json');

        let config;

        if (fs.existsSync(configFile)) {
            config = require(configFile);

        } else {
            config = {};
        }

        if (config.nodejs === null || typeof config.nodejs !== 'object') {
            config.nodejs = {};
        }

        config.nodejs.env = config.nodejs.env || 'production';
        config.nodejs.host = config.nodejs.host || '0.0.0.0';
        config.nodejs.port = config.nodejs.port || 3000;

        if (config.sequelize === null || typeof config.sequelize !== 'object') {
            config.sequelize = {};
        }

        config.sequelize.database = config.sequelize.database || process.env.DB_NAME || null;
        config.sequelize.dialect = config.sequelize.dialect || process.env.DB_DIALECT || 'sqlite';

        if (config.sequelize.dialectOptions === null || typeof config.sequelize.dialectOptions !== 'object') {
            config.sequelize.dialectOptions = {};
        }

        config.sequelize.dialectOptions.timezone = config.sequelize.dialectOptions.timezone || 'Etc/GMT0';
        config.sequelize.host = config.sequelize.host || process.env.DB_HOST || null;
        config.sequelize.logging = false;
        config.sequelize.password = config.sequelize.password || process.env.DB_PASS || null;
        config.sequelize.port = config.sequelize.port || process.env.DB_PORT || null;
        config.sequelize.storage = config.sequelize.storage || process.env.DB_STORAGE || 'database.sqlite';
        config.sequelize.username = config.sequelize.username || process.env.DB_USER || null;

        return config;
    }

    constructor() {
        const staticDir = process.env.CLIENT_HTML_DIR || '../client/dist/rdio-scanner';
        const staticFile = 'index.html';

        this.config = App.Config();

        this.router = express();
        this.router.use(express.json());
        this.router.use(express.urlencoded({ extended: false }));
        this.router.use(express.static(staticDir));
        this.router.set(process.env.PORT || this.config.nodejs.port);

        if (this.config.nodejs.env !== 'development') {
            this.router.disable('x-powered-by');

            this.router.get(/\/|\/index.html/, cors(), (_, res) => {
                if (fs.existsSync(path.join(staticDir, staticFile))) {
                    return res.sendFile(staticFile, { root: staticDir });

                } else {
                    return res.send('A new build is being prepared. Please check back in a few minutes.');
                }
            });

            this.router.use(helmet());
        }

        if (this.config.nodejs.env !== 'development' && fs.existsSync(this.config.nodejs.sslCert) && fs.existsSync(this.config.nodejs.sslKey)) {
            this.httpServer = https.createServer({
                cert: fs.readFileSync(this.config.nodejs.sslCert),
                key: fs.readFileSync(this.config.nodejs.sslKey),
            }, this.router);

            this.httpServer.listen(this.config.nodejs.port, this.config.nodejs.host, () => {
                console.log(`Server is running at https://${this.config.nodejs.host}:${this.config.nodejs.port}/`);
            });

        } else {
            this.httpServer = http.createServer(this.router);

            this.httpServer.listen(this.config.nodejs.port, this.config.nodejs.host, () => {
                console.log(`Server is running at http://${this.config.nodejs.host}:${this.config.nodejs.port}/`);
            });
        }

        this.sequelize = new Sequelize(this.config.sequelize);

        this.models = {};

        this.routes = {};

        this.rdioScanner = new RdioScanner(this);
    }
}

if (require.main === module) {
    new App();
}

module.exports = App;
