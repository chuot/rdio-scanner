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
                    return talkgroup === undefined || talkgroup === '*';
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
                return false;
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
        form.append('system', this.getSystemId(call, downstream));
        form.append('talkgroup', this.getTalkgroupId(call, downstream));

        form.submit(apiUrl, (error, response) => {
            const message = `Downstream: system=${call.system} talkgroup=${call.talkgroup} file=${call.audioName} to=${downstream.url}`;

            if (error) {
                console.error(`${message} ${error.message}`);

            } else if (response.statusCode !== 200) {
                console.error(`${message} ${response.statusMessage}`);

            } else {
                console.log(`${message} Success`);
            }
        });
    }

    getSystemId(call, downstream) {
        const parseSystem = (system) => {
            if (Array.isArray(system)) {
                system = system.find((sys) => sys !== null && typeof sys === 'object' && sys.id === call.system);

                return system ? parseSystem(system) : call.system;

            } else if (system !== null && typeof system === 'object') {
                return typeof system.id_as === 'number' && system.id === call.system ? system.id_as : call.system;

            } else {
                return call.system;
            }
        }

        return parseSystem(downstream.systems);
    }

    getTalkgroupId(call, downstream) {
        const parseSystem = (system) => {
            const parseTalkgroup = (talkgroup) => {
                if (Array.isArray(talkgroup)) {
                    talkgroup = talkgroup.find((tg) => tg !== null && typeof tg === 'object' && tg.id === call.talkgroup);

                    return talkgroup ? parseTalkgroup(talkgroup) : call.talkgroup;

                } else if (talkgroup !== null && typeof talkgroup === 'object') {
                    return typeof talkgroup.id_as === 'number' && talkgroup.id === call.talkgroup ? talkgroup.id_as : talkgroup.id;

                } else {
                    return call.talkgroup;
                }
            }

            if (Array.isArray(system)) {
                system = system.find((sys) => sys !== null && typeof sys === 'object' && sys.id === call.system);

                return system ? parseSystem(system) : call.talkgroup;

            } else if (system !== null && typeof system === 'object') {
                return system.id === call.system ? parseTalkgroup(system.talkgroups) : call.talkgroup;

            } else {
                return call.talkgroup;
            }
        }

        return parseSystem(downstream.systems);
    }
}

module.exports = Downstream;
