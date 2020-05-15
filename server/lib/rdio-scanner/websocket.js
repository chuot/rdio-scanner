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

const { Server } = require('http');
const url = require('url');
const WS = require('ws');

class WebSocket {
    constructor(app) {
        this.heartbeat = setInterval(() => {
            this.wss.clients.forEach((ws) => {
                if (ws.isAlive === false) {
                    ws.terminate();

                    this.logClientsCount();
                }

                ws.isAlive = false;

                ws.ping();
            });
        }, 15 * 60 * 1000);

        this.wss = new WS.Server({ noServer: true });

        this.wss.on('connection', (ws) => {
            ws.isAlive = true;

            ws.on('close', () => this.logClientsCount());

            ws.on('error', (error) => console.error(error));

            ws.on('message', async (message) => await app.controller.messageParser(ws, message));

            ws.on('pong', () => ws.isAlive = true);

            this.logClientsCount();
        });

        this.wss.on('close', () => {
            clearInterval(this.heartbeat);

            this.heartbeat = undefined;
        });

        if (app.httpServer instanceof Server) {
            app.httpServer.on('upgrade', (request, socket, head) => {
                const pathname = url.parse(request.url).pathname;

                if (pathname === '/' || pathname === '/index.html') {
                    this.wss.handleUpgrade(request, socket, head, (ws) => this.wss.emit('connection', ws, request));
                }
            });
        }
    }

    logClientsCount() {
        console.log(`WebSocket: Listeners count is ${this.wss.clients.size}`);
    }
}

module.exports = WebSocket;
