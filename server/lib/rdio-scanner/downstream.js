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
const FormData = require('form-data');
const url = require('url');

class Downstream {
    constructor(ctx = {}) {
        if (!Array.isArray(ctx.config.downstreams)) {
            ctx.config.downstreams = [];
        }

        this.config = ctx.config.downstreams;

        this.config.forEach((downstream) => {
            if (typeof downstream.apiKey !== 'string' || !downstream.apiKey.length) {
                console.error(`Config: no dirWatch.apiKey defined`);
            }

            if (typeof downstream.disabled !== 'boolean') {
                downstream.disabled = false;
            }

            if (downstream.systems === undefined) {
                downstream.systems = '*';
            }

            if (typeof downstream.url !== 'string' || !downstream.url.length) {
                console.error(`Config: no dirWatch.url defined`)
            }
        });

        if (ctx.controller instanceof EventEmitter) {
            ctx.controller.on('call', (call) => this.exportCall(call));
        }
    }

    exportCall(call) {
        const parseSystem = (system) => {
            const parseTalkgroup = (talkgroup) => {
                if (Array.isArray(talkgroup)) {
                    return talkgroup.some((tg) => parseTalkgroup(tg));

                } else if (talkgroup !== null && typeof talkgroup === 'object') {
                    return talkgroup.id === call.talkgroup;

                } else if (typeof talkgroup === 'number') {
                    return talkgroup === call.talkgroup;

                } else {
                    return talkgroup === '*';
                }
            };

            if (Array.isArray(system)) {
                return system.some((sys) => parseSystem(sys));

            } else if (system !== null && typeof system === 'object' && system.id === call.system) {
                return parseTalkgroup(system.talkgroups);

            } else if (typeof system === 'number') {
                return system === call.system;

            } else if (typeof system === 'string') {
                return system === '*';

            } else {
                return true;
            }
        };

        this.config.forEach((downstream) => {
            if (typeof downstream.disabled === 'boolean' && downstream.disabled) {
                return;
            }

            if (typeof downstream.apiKey !== 'string' || !downstream.apiKey.length) {
                return;
            }

            if (typeof downstream.url !== 'string' || !downstream.url.length) {
                return;
            }

            if (parseSystem(downstream.systems)) {
                switch (downstream.type) {
                    default:
                        this.exportCallToRdioScanner(call, downstream);
                }
            }
        });
    }

    exportCallToRdioScanner(call, downstream) {
        const apiUrl = url.resolve(downstream.url, '/api/call-upload');

        const form = new FormData();

        form.append('key', downstream.apiKey);
        form.append('audio', call.audio, {
            contentType: call.audioType,
            filename: call.audioName,
            knownLength: call.audio.length,
        });
        form.append('dateTime', call.dateTime.toJSON());
        form.append('frequencies', JSON.stringify(call.frequencies));

        if (typeof call.frequency === 'number') {
            form.append('frequency', call.frequency);
        }

        if (typeof call.source === 'number') {
            form.append('source', call.source);
        }

        form.append('sources', JSON.stringify(call.sources));
        form.append('system', call.system);
        form.append('talkgroup', call.talkgroup);

        form.submit(apiUrl, (error) => {
            if (error) {
                console.log(`Downstream: system=${call.system} talkgroup=${call.talkgroup} file=${call.audioName} to=${downstream.url} ${error.message}`);

            } else {
                console.log(`Downstream: system=${call.system} talkgroup=${call.talkgroup} file=${call.audioName} to=${downstream.url} Success`)
            }
        });
    }
}

module.exports = Downstream;
