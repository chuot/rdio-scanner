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

import WS from 'ws';

import { Log } from '../../log.js';

export class Config {
    constructor(ctx) {
        this.admin = ctx.admin;

        this.config = ctx.config;

        this.log = ctx.log;

        this.path = '/api/admin/config';

        this.wss = new WS.Server({ noServer: true });

        this.wss.on('connection', (ws) => {
            const timeout = setTimeout(() => ws.close(), 10000);

            ws.isAlive = true;

            ws.on('message', (message) => {
                clearTimeout(timeout);

                if (this.admin.validateToken(message)) {
                    ws.authenticated = true;
                } else {
                    ws.close(1000);
                }
            });

            ws.on('pong', () => ws.isAlive = true);
        });

        this.wss.on('close', () => {
            clearInterval(this.heartbeat);

            this.heartbeat = undefined;
        });

        this.config.on('config', (config) => {
            this.wss.clients.forEach(async (ws) => {
                if (ws.authenticated) {
                    ws.send(JSON.stringify(config));
                }
            });
        });

        this.heartbeat = setInterval(() => {
            this.wss.clients.forEach((ws) => {
                if (ws.isAlive === false) {
                    ws.terminate();

                    this.logClientsCount();

                } else {
                    ws.isAlive = false;

                    ws.ping();
                }
            });
        }, 30000);

        ctx.httpServer.on('upgrade', (request, socket, head) => {
            const pathname = new URL(request.url, `http://${request.headers.host}`).pathname;

            if (pathname === this.path) {
                this.wss.handleUpgrade(request, socket, head, (ws) => this.wss.emit('connection', ws, request));
            }
        });

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
            if (!req.auth.valid) {
                return res.sendStatus(401);
            }

            res.send(getConfig(this));
        };
    }

    patch() {
        return (req, res) => {
            res.sendStatus(405);
        };
    }

    post() {
        return (req, res) => {
            res.sendStatus(405);
        };
    }

    put() {
        return async (req, res) => {
            if (!req.auth.valid) {
                return res.sendStatus(401);
            }

            if (
                !Array.isArray(req.body.access) ||
                !Array.isArray(req.body.apiKeys) ||
                !Array.isArray(req.body.dirWatch) ||
                !Array.isArray(req.body.downstreams) ||
                !Array.isArray(req.body.groups) ||
                req.body.options === null || typeof req.body.options !== 'object' ||
                !Array.isArray(req.body.systems) ||
                !Array.isArray(req.body.tags)
            ) {
                return res.sendStatus(400);
            }

            this.config.access = req.body.access.map((access) => Object.assign({}, access, {
                expiration: typeof access.expiration === 'string' && access.expiration.length
                    ? new Date(access.expiration) : null,
            }));
            this.config.apiKeys = req.body.apiKeys;
            this.config.dirWatch = req.body.dirWatch;
            this.config.downstreams = req.body.downstreams;
            this.config.groups = req.body.groups;
            this.config.options = req.body.options;
            this.config.systems = req.body.systems.map((system) => Object.assign({}, system, {
                blacklists: typeof system.blacklists === 'string'
                    ? system.blacklists
                        .split(',')
                        .map((blacklist) => parseInt(blacklist, 10))
                        .filter((blacklist) => typeof blacklist === 'number' && !isNaN(blacklist))
                    : Array.isArray(system.blacklists)
                        ? system.blacklists
                        : null,
            }));
            this.config.tags = req.body.tags;

            this.log.write(Log.info, 'Admin: Configuration changed');

            res.send(getConfig(this));
        };
    }
}

function getConfig(ctx) {
    const config = {
        config: {
            access: ctx.config.access,
            apiKeys: ctx.config.apiKeys,
            dirWatch: ctx.config.dirWatch,
            docker: 'DOCKER' in process.env,
            downstreams: ctx.config.downstreams,
            groups: ctx.config.groups,
            options: ctx.config.options,
            systems: ctx.config.systems.map((system) => Object.assign({}, system, { blacklists: system.blacklists.join(',') })),
            tags: ctx.config.tags,
        },
    };

    if (ctx.config.adminPasswordNeedChange) {
        config.passwordNeedChange = true;
    }

    return config;
}
