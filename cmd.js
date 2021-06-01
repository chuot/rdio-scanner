#!/usr/bin/env node

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

import fs from 'fs';
import http from 'http';
import https from 'https';
import path from 'path';

const APP_NAME = path.parse(process.argv[1]).base;

const ARGUMENT_CODE = '--code';
const ARGUMENT_IDENT = '--ident';
const ARGUMENT_IN = '--in';
const ARGUMENT_OUT = '--out';
const ARGUMENT_PASSWORD = '--password';
const ARGUMENT_TOKEN = '--token';
const ARGUMENT_URL = '--url';

const COMMAND_CONFIG_GET = 'config-get';
const COMMAND_CONFIG_SET = 'config-set';
const COMMAND_HELP = 'help';
const COMMAND_LOGIN = 'login';
const COMMAND_LOGOUT = 'logout';
const COMMAND_USER_ADD = 'user-add';
const COMMAND_USER_REMOVE = 'user-remove';

const DEFAULT_ADMIN_PASSWORD = 'rdio-scanner';
const DEFAULT_URL = 'http://localhost:3000/';
const DEFAULT_TOKEN_FILE = `.${path.parse(process.argv[1]).name}.token`;

const PADDING = 11;

const URL_API = '/api/admin';
const URL_CONFIG = `${URL_API}/config`;
const URL_LOGIN = `${URL_API}/login`;
const URL_LOGOUT = `${URL_API}/logout`;

let tokenFile = DEFAULT_TOKEN_FILE;

export class Cmd {
    constructor() {
        this.command = null;
        this.inputFile = null;
        this.outputFile = null;
        this.password = process.env.RDIO_ADMIN_PASSWORD || DEFAULT_ADMIN_PASSWORD;
        this.token = null;
        this.url = new URL(DEFAULT_URL);

        const args = process.argv.slice(2).reduce((p, c) => p.concat(c.split(/(--\w+)=(\S+)/).filter((f) => f)), []);

        for (let i = 0; i < args.length; i++) {
            switch (args[i]) {
                case COMMAND_CONFIG_GET:
                case COMMAND_CONFIG_SET:
                case COMMAND_HELP:
                case COMMAND_LOGIN:
                case COMMAND_LOGOUT:
                case COMMAND_USER_ADD:
                case COMMAND_USER_REMOVE:
                    this.command = args[i];
                    break;

                case ARGUMENT_CODE:
                    this.code = args[++i];
                    break;

                case ARGUMENT_IDENT:
                    this.ident = args[++i];
                    break;

                case ARGUMENT_IN:
                    this.inputFile = args[++i];
                    break;

                case ARGUMENT_OUT:
                    this.outputFile = args[++i];
                    break;

                case ARGUMENT_PASSWORD:
                    this.password = args[++i];
                    break;

                case ARGUMENT_TOKEN:
                    tokenFile = args[++i];
                    break;

                case ARGUMENT_URL:
                    try {
                        this.url = new URL(args[++i]);
                    } catch (error) {
                        exitWithError('Invalid URL');
                    }
                    if (!/^https?:/i.test(this.url)) {
                        exitWithError('Invalid URL');
                    }
                    break;
            }
        }

        this.token = readTokenFile();

        switch (this.command) {
            case COMMAND_CONFIG_GET:
                this.configGet();
                break;

            case COMMAND_CONFIG_SET:
                this.configSet();
                break;

            case COMMAND_LOGIN:
                this.login();
                break;

            case COMMAND_LOGOUT:
                this.logout();
                break;

            case COMMAND_USER_ADD:
                this.userAdd();
                break;

            case COMMAND_USER_REMOVE:
                this.userRemove();
                break;

            default:
                this.usage();
        }
    }

    async configGet() {
        if (!this.outputFile) {
            exitWithError('Missing --out <file.json> arguments.');
            return;
        }

        const res = await httpGet(new URL(URL_CONFIG, this.url), this.token);

        if (res.statusCode === 200) {
            saveFile(this.outputFile, JSON.stringify(res.data.config, null, 2));
            exitWithMessage(`Server's configuration saved to ${this.outputFile}`);
        } else {
            exitWithError(res.error || res.data);
        }
    }

    async configSet() {
        if (!this.inputFile) {
            exitWithError('Missing --in <file.json> arguments.');
            return;
        }

        let config = readFile(this.inputFile);

        try {
            config = JSON.parse(config);
        } catch (error) {
            exitWithError(error.message);
            return;
        }

        const res = await httpPut(new URL(URL_CONFIG, this.url), config, this.token);

        if (res.statusCode === 200) {
            exitWithMessage(`Server's configuration applied from file ${this.inputFile}`);

        } else {
            exitWithError(res.error || res.data);
        }
    }

    async login() {
        const res = await httpPost(new URL(URL_LOGIN, this.url), { password: this.password });

        if (res.statusCode === 200) {
            this.token = res.data.token;
            saveTokenFile(this.token);
            exitWithMessage('Logged in.');

        } else {
            exitWithError(res.error || res.data);
        }
    }

    async logout() {
        const res = await httpPost(new URL(URL_LOGOUT, this.url), null, this.token);

        this.token = null;
        saveTokenFile(this.token);

        if (res.statusCode === 200) {
            exitWithMessage('Logged out.');

        } else {
            exitWithError(res.error || res.data);
        }
    }

    usage() {
        exitWithMessage([
            'Available Commands:',
            `  ${COMMAND_CONFIG_GET.padEnd(PADDING)} – Retrieve server's configuration.`,
            '',
            `    ${''.padStart(PADDING)} $ ./${APP_NAME} ${COMMAND_CONFIG_GET} ${ARGUMENT_OUT} <file.json>`,
            '',
            `  ${COMMAND_CONFIG_SET.padEnd(PADDING)} – Set server's configuration.`,
            '',
            `    ${''.padStart(PADDING)} $ ./${APP_NAME} ${COMMAND_CONFIG_SET} ${ARGUMENT_IN} <file.json>`,
            '',
            `  ${COMMAND_LOGIN.padEnd(PADDING)} – Login to administrative backend.`,
            '',
            `    ${''.padStart(PADDING)} $ RDIO_ADMIN_PASSWORD=<password> ./${APP_NAME} ${COMMAND_LOGIN}`,
            `    ${''.padStart(PADDING)} $ ./${APP_NAME} ${COMMAND_LOGIN} ${ARGUMENT_PASSWORD} <password>`,
            '',
            `  ${COMMAND_LOGOUT.padEnd(PADDING)} – Logout from administrative backend.`,
            '',
            `  ${COMMAND_USER_ADD.padEnd(PADDING)} – Add a user access.`,
            '',
            `    ${''.padStart(PADDING)} $ ./${APP_NAME} ${COMMAND_USER_ADD} ${ARGUMENT_IDENT} <ident> ${ARGUMENT_CODE} <code>`,
            '',
            `  ${COMMAND_USER_REMOVE.padEnd(PADDING)} – Remove a user access.`,
            '',
            `    ${''.padStart(PADDING)} $ ./${APP_NAME} ${COMMAND_USER_REMOVE} ${ARGUMENT_IDENT} <ident>`,
            '',
            'Global options:',
            `  ${ARGUMENT_TOKEN.padEnd(PADDING)} – Session token keystore. Default is '${DEFAULT_TOKEN_FILE}'.`,
            `  ${ARGUMENT_URL.padEnd(PADDING)} – Server remote address. Default is '${DEFAULT_URL}'.`,
            '',
        ].join('\n'));
    }

    async userAdd() {
        if (!this.code || !this.ident) {
            exitWithError('Missing --ident <ident> or --code <code> arguments.');
            return;
        }

        let res = await httpGet(new URL(URL_CONFIG, this.url), this.token);

        if (res.statusCode !== 200) {
            exitWithError(res.error || res.data);
            return;
        }

        const config = res.data.config;

        config.access = config.access.filter((u) => u.ident.toLowerCase() !== this.ident.toLowerCase());

        config.access = config.access.concat({
            code: this.code,
            ident: this.ident,
            systems: '*',
        });

        res = await httpPut(new URL(URL_CONFIG, this.url), config, this.token);

        if (res.statusCode === 200) {
            exitWithMessage(`User ${this.ident} added.`);

        } else {
            exitWithError(res.error || res.data);
        }
    }

    async userRemove() {
        if (!this.ident) {
            exitWithError('Missing --ident <ident> arguments.');
            return;
        }

        let res = await httpGet(new URL(URL_CONFIG, this.url), this.token);

        if (res.statusCode !== 200) {
            exitWithError(res.error || res.data);
            return;
        }

        const config = res.data.config;

        if (config.access.findIndex((u) => u.ident.toLowerCase() === this.ident.toLowerCase()) === -1) {
            exitWithError(`Unknown user ${this.ident}`);
            return;
        }

        config.access = config.access.filter((u) => u.ident.toLowerCase() !== this.ident.toLowerCase());

        res = await httpPut(new URL(URL_CONFIG, this.url), config, this.token);

        if (res.statusCode === 200) {
            exitWithMessage(`User ${this.ident} removed.`);

        } else {
            exitWithError(res.error || res.data);
        }
    }
}

function exitWithError(error) {
    process.stdout.write(`ERROR: ${error}\n`);
    process.exit(1);
}

function exitWithMessage(message) {
    process.stdout.write(`${message}\n`);
    process.exit(0);
}

async function httpGet(url, autorization) {
    return new Promise((resolve) => {
        const result = { data: null, error: null, statusCode: null };
        const options = autorization ? { headers: { Authorization: autorization } } : {};

        try {
            const req = (url.protocol === 'https:' ? https : http).get(url, options, (res) => {
                result.statusCode = res.statusCode;

                res.setEncoding('utf8');
                res.on('data', (chunk) => result.data = (result.data || '') + chunk);
                res.on('end', () => {
                    try {
                        result.data = JSON.parse(result.data);
                    } catch (error) { }
                    resolve(result);
                });
            });

            req.on('error', (error) => {
                result.error = error.code;
                resolve(result);
            });

        } catch (error) {
            result.error = error.message;
            resolve(result);
        }
    });
}

async function httpPost(url, postData, autorization) {
    return new Promise((resolve) => {
        const result = { data: null, error: null, statusCode: null };

        postData = JSON.stringify(postData || {});

        const options = {
            headers: {
                'Content-Type': 'application/json',
                'Content-Length': Buffer.byteLength(postData),
            },
            method: 'POST',
        };

        if (autorization) {
            options.headers.Authorization = autorization;
        }

        try {
            const req = (url.protocol === 'https:' ? https : http).request(url, options, (res) => {
                result.statusCode = res.statusCode;

                res.setEncoding('utf8');
                res.on('data', (chunk) => result.data = (result.data || '') + chunk);
                res.on('end', () => {
                    try {
                        result.data = JSON.parse(result.data);
                    } catch (error) { }

                    resolve(result);
                });
            });

            req.on('error', (error) => {
                result.error = error.code;
                resolve(result);
            });
            req.write(postData);
            req.end();

        } catch (error) {
            result.error = error.message;
            resolve(result);
        }
    });
}

async function httpPut(url, putData, autorization) {
    return new Promise((resolve) => {
        const result = { data: null, error: null, statusCode: null };

        putData = JSON.stringify(putData);

        const options = {
            headers: {
                'Content-Type': 'application/json',
                'Content-Length': Buffer.byteLength(putData),
            },
            method: 'PUT',
        };

        if (autorization) {
            options.headers.Authorization = autorization;
        }

        try {
            const req = (url.protocol === 'https:' ? https : http).request(url, options, (res) => {
                result.statusCode = res.statusCode;

                res.setEncoding('utf8');
                res.on('data', (chunk) => result.data = (result.data || '') + chunk);
                res.on('end', () => {
                    try {
                        result.data = JSON.parse(result.data);
                    } catch (error) { }

                    resolve(result);
                });
            });

            req.on('error', (error) => {
                result.error = error.code;
                resolve(result);
            });
            req.write(putData);
            req.end();

        } catch (error) {
            result.error = error.message;
            resolve(result);
        }
    });
}

function readFile(file) {
    if (fs.existsSync(file)) {
        try {
            return fs.readFileSync(file);

        } catch (error) {
            exitWithError(error.message);
        }

    } else {
            exitWithError(`Inexistent token keystore file ${file}`);
    }
}

function readTokenFile() {
    return readFile(tokenFile);
}

function saveFile(file, data) {
    try {
        fs.writeFileSync(file, data || '');

    } catch (error) {
        exitWithError(error.message);
    }
}

function saveTokenFile(token) {
    saveFile(tokenFile, token);
}

new Cmd();