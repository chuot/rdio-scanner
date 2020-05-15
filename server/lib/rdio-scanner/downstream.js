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

const FormData = require('form-data');
const url = require('url');

class Downstream {
    constructor(app = {}) {
        this.app = app;
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

            } else {
                return system === '*';
            }
        };

        this.app.config.downstreams.forEach((downstream) => {
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
        form.append('frequency', call.frequency);
        form.append('source', call.source || '');
        form.append('sources', JSON.stringify(call.sources));
        form.append('system', call.system);
        form.append('talkgroup', call.talkgroup);

        form.submit(apiUrl, (error) => {
            if (error) {
                console.log(`Downstream: system=${call.system} talkgroup=${call.talkgroup} file=${call.audioName} to=${downstream.url} ${error.message}`);
            }

            console.log(`Downstream: system=${call.system} talkgroup=${call.talkgroup} file=${call.audioName} to=${downstream.url} Success`)
        });
    }
}

module.exports = Downstream;
