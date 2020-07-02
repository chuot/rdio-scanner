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

const { spawn, spawnSync } = require('child_process');

const EventEmitter = require('events');

const { Op } = require('sequelize');

const maxAuthenticationTries = 3;

const wsCommand = {
    call: 'CAL',
    config: 'CFG',
    listCall: 'LCL',
    liveFeedMap: 'LFM',
    nop: 'NOP',
    pin: 'PIN',
};

class Controller {
    get isAccessRestricted() {
        return this.app.config.access !== null && this.app.config.access !== undefined;
    }

    constructor(app = {}) {
        this.app = app;

        this.eventEmitter = new EventEmitter();

        this.eventEmitter.setMaxListeners(0);

        this.ffmpeg = !spawnSync('ffmpeg', ['-version']).error;

        if (!this.ffmpeg) {
            console.warn('ffmpeg is missing, no audio conversion possible.');
        }

        if (this.app.config.pruneDays > 0) {
            this.pruneInterval = setInterval(() => {
                const now = new Date();

                this.app.models.call.destroy({
                    where: {
                        dateTime: {
                            [Op.lt]: new Date(now.getFullYear(), now.getMonth(), now.getDate() - this.app.config.pruneDays),
                        },
                    },
                });
            }, 15 * 60 * 1000);

        } else {
            this.pruneInterval = undefined;
        }
    }

    authenticate(token) {
        const parse = (doc) => {
            if (!this.isAccessRestricted) {
                return true;

            } else if (Array.isArray(doc)) {
                return doc.map((acc) => parse(acc)).some((acc) => acc);

            } else if (doc !== null && typeof doc === 'object') {
                return doc.code === token;

            } else {
                return doc === token;
            }
        }

        return parse(this.app.config.access);
    }

    convertCallAudio(call = {}) {
        return new Promise((resolve, reject) => {
            if (!this.ffmpeg) {
                return reject('No ffmpeg available');
            }

            if (Buffer.isBuffer(call && call.audio)) {
                const proc = spawn('ffmpeg', [
                    '-i',
                    '-',
                    '-c:a',
                    'aac',
                    '-b:a',
                    '32k',
                    '-movflags',
                    'frag_keyframe+empty_moov',
                    '-f',
                    'ipod',
                    '-',
                ]);

                let audio = Buffer.from([]);

                proc.on('close', () => {
                    call.audio = audio;
                    call.audioName = call.audioName ? call.audioName.replace(/\.[^.]+$/, '.m4a') : 'audio.m4a';
                    call.audioType = 'audio/mp4';

                    resolve(call);
                });

                proc.on('error', (error) => reject(error));

                proc.stdin.on('error', (error) => reject(error.message));

                proc.stdout.on('data', (data) => audio = Buffer.concat([audio, data]));

                process.nextTick(() => {
                    proc.stdin.setEncoding('binary');
                    proc.stdin.write(call.audio);
                    proc.stdin.end();
                });
            }
        });
    }

    async getCall(id, scope) {
        const where = scope !== null && typeof scope === 'object' ? {
            [Op.and]: [
                { id },
                {
                    [Op.or]: Object.keys(scope).map((sys) => ({
                        system: sys,
                        talkgroup: {
                            [Op.in]: scope[sys],
                        },
                    }), []),
                },
            ],
        } : { id };

        const call = await this.app.models.call.findOne({ where });

        return call || null;
    }

    async getCalls(options, scope) {
        const attributes = ['id', 'dateTime', 'system', 'talkgroup'];

        const date = options && typeof options.date === 'string' ? new Date(options.date) : null;

        const limit = options && typeof options.limit === 'number' ? Math.min(500, options.limit) : 500;

        const offset = options && typeof options.offset === 'number' ? options.offset : 0;

        const order = [['dateTime', options && typeof options.sort === 'number' && options.sort < 0 ? 'DESC' : 'ASC']];

        const system = options && typeof options.system === 'number' ? options.system : null;

        const talkgroup = options && typeof options.talkgroup === 'number' ? options.talkgroup : null;

        let where1 = null;

        if (scope !== null && typeof scope === 'object') {
            where1 = {
                [Op.or]: Object.keys(scope).map((sys) => ({
                    system: sys,
                    talkgroup: {
                        [Op.in]: scope[sys],
                    },
                }), []),
            };
        }

        if (system && talkgroup) {
            where1 = {
                [Op.and]: where1 ? [where1, { system }, { talkgroup }] : [{ system }, { talkgroup }],
            };

        } else if (system) {
            where1 = {
                [Op.and]: where1 ? [where1, { system }] : [{ system }],
            };
        }

        const where2 = date ? {
            [Op.and]: [
                where1,
                {
                    dateTime: {
                        [Op.gte]: new Date(date.getFullYear(), date.getMonth(), date.getDate(), 0, 0, 0),
                        [Op.lte]: new Date(date.getFullYear(), date.getMonth(), date.getDate(), 23, 59, 59),
                    },
                },
            ],
        } : where1;

        const [dateStartQuery, dateStopQuery, count, results] = await Promise.all([
            await this.app.models.call.findOne({ attributes: ['dateTime'], order: [['dateTime', 'ASC']], where: where1 }),
            await this.app.models.call.findOne({ attributes: ['dateTime'], order: [['dateTime', 'DESC']], where: where1 }),
            await this.app.models.call.count({ where: where2 }),
            await this.app.models.call.findAll({ attributes, limit, offset, order, where: where2 }),
        ]);

        const dateStart = dateStartQuery && dateStartQuery.get('dateTime');

        const dateStop = dateStopQuery && dateStopQuery.get('dateTime');

        return { count, dateStart, dateStop, results };
    }

    async getConfig(scope) {
        return Object.assign({}, this.getOptions(), {
            allowDownload: this.app.config.allowDownload,
            systems: await this.getSystems(scope),
        });
    }

    getOptions() {
        return {
            useDimmer: this.app.config.useDimmer,
            useGroup: this.app.config.useGroup,
            useLed: this.app.config.useLed,
        };
    }

    getScope(token, store = this.app.config.access) {
        const parse = (record, first = true) => {
            const parseSystem = (system, first = true) => {
                const parseTalkgroup = (talkgroup, first = true) => {
                    if (Array.isArray(talkgroup)) {
                        return talkgroup.map((tg) => parseTalkgroup(tg, false));

                    } else if (talkgroup !== null && typeof talkgroup === 'object' && typeof talkgroup.id === 'number') {
                        return first ? [talkgroup.id] : talkgroup.id;

                    } else if (talkgroup !== null && talkgroup !== undefined) {
                        return first ? [talkgroup] : talkgroup;

                    } else {
                        return [];
                    }
                };

                if (Array.isArray(system)) {
                    return system.map((sys) => parseSystem(sys, false)).filter((sys) => sys.length);

                } else if (system !== null && typeof system === 'object' && typeof system.id === 'number') {
                    if (this.app.config.systems.find((sys) => sys.id === system.id)) {
                        const talkgroups = parseTalkgroup(system.talkgroups);

                        if (talkgroups.length) {
                            return first ? [[system.id, talkgroups]] : [system.id, talkgroups];

                        } else {
                            return [];
                        }

                    } else {
                        return [];
                    }

                } else if (typeof system === 'number') {
                    if (this.app.config.systems.find((sys) => sys.id === system)) {
                        const systems = parseSystem(this.app.config.systems.find((sys) => sys.id === system), false);

                        return first ? [systems] : systems;

                    } else {
                        return [];
                    }

                } else if (system === '*') {
                    return parseSystem(this.app.config.systems);

                } else {
                    return [];
                }
            };

            if (record === null || record === undefined) {
                return this.isAccessRestricted ? [] : parseSystem(this.app.config.systems);

            } else if (Array.isArray(record)) {
                return parse(record.find((acc) => acc === token || acc.code === token || acc.key === token));

            } else if (record !== null && typeof record === 'object' && (record.code === token || record.key === token)) {
                const systems = parseSystem(record.systems);
                return first ? systems : systems;

            } else if (record === token) {
                return parseSystem(this.app.config.systems);

            } else {
                return [];
            }
        }

        return parse(store).reduce((obj, arr) => {
            obj[arr[0]] = arr[1];

            return obj;
        }, {});
    }

    async getSystems(scope = {}) {
        if (scope !== null && typeof scope === 'object') {
            const systems = await this.app.models.system.findAll({
                where: {
                    id: {
                        [Op.in]: Object.keys(scope),
                    },
                },
            });

            return systems.map((system) => (system.talkgroups = system.talkgroups.filter((tg) => scope[system.id].includes(tg.id))) && system);

        } else {
            const systems = await this.app.models.system.findAll();

            return systems;
        }
    }

    async importCall(call = {}) {
        const system = this.app.config.systems.find((sys) => sys.id == call.system);

        const talkgroup = Array.isArray(system && system.talkgroups) ? system.talkgroups.find((tg) => tg.id == call.talkgroup) : null;

        if (!system || !talkgroup) {
            console.log(`NewCall: system=${call.system || 'unknown'} talkgroup=${call.talkgroup || 'unknown'} file=${call.audioName} No matching system/talkgroup`);

            return;
        }

        if (call.audioType !== 'audio/aac') {
            try {
                call = await this.convertCallAudio(call);

            } catch (error) {
                console.log(`NewCall: system=${call.system} talkgroup=${call.talkgroup} file=${call.audioName} ${error.message}`);
            }
        }

        const newCall = await this.app.models.call.create(call);

        console.log(`NewCall: system=${call.system} talkgroup=${call.talkgroup} file=${call.audioName} Success`);

        this.eventEmitter.emit('call', newCall);

        this.app.downstream.exportCall(call);
    }

    async importTrunkRecorder(audio, audioName, audioType, system, meta) {
        const parseDate = (value) => {
            const date = new Date(1970, 0, 1);

            date.setUTCSeconds(value - date.getTimezoneOffset() * 60);

            return date;
        };

        await this.importCall({
            audio,
            audioName,
            audioType,
            dateTime: parseDate(meta.start_time),
            frequencies: Array.isArray(meta.freqList) ? meta.freqList.map((f) => ({
                errorCount: f.error_count,
                freq: f.freq,
                len: f.len,
                pos: f.pos,
                spikeCount: f.spike_count,
            })) : [],
            frequency: parseInt(meta.freq, 10),
            sources: Array.isArray(meta.srcList) ? meta.srcList.map((s) => ({
                pos: s.pos,
                src: s.src,
            })) : [],
            system,
            talkgroup: meta.talkgroup,
        });
    }

    async messageParser(socket, message) {
        try {
            message = JSON.parse(message);

        } catch (_) {
            message = [];
        }

        if (Array.isArray(message)) {
            if (!this.isAccessRestricted && socket.scope === undefined) {
                socket.scope = this.getScope();
            }

            if (this.isAccessRestricted && !socket.isAuthenticated && message[0] !== wsCommand.pin) {
                socket.send(JSON.stringify([wsCommand.config, await this.getOptions()]));

                socket.send(JSON.stringify([wsCommand.pin]));

            } else if (message[0] === wsCommand.call) {
                const call = await this.getCall(message[1], socket.scope);

                const response = [wsCommand.call, call];

                if (message[2]) {
                    response.push(message[2]);
                }

                socket.send(JSON.stringify(response));

            } else if (message[0] === wsCommand.config) {
                socket.send(JSON.stringify([wsCommand.config, await this.getConfig(socket.scope)]));

            } else if (message[0] === wsCommand.listCall) {
                socket.send(JSON.stringify([wsCommand.listCall, await this.getCalls(message[1], socket.scope)]));

            } else if (message[0] === wsCommand.liveFeedMap) {
                let returnStatus;

                if (message[1] !== null && typeof message[1] === 'object') {
                    const allOff = Object.keys(message[1]).every((sys) => Object.keys(message[1][sys]).every((tg) => !message[1][sys][tg]));

                    if (allOff) {
                        if (typeof socket.liveFeed === 'function') {
                            this.eventEmitter.removeListener('call', socket.liveFeed);
                        }

                        socket.liveFeed = undefined;

                        returnStatus = true;

                    } else {
                        if (typeof socket.liveFeed === 'function') {
                            this.eventEmitter.removeListener('call', socket.liveFeed);
                        }

                        socket.liveFeed = (call) => {
                            if (socket.readyState !== 3) {
                                if (call.system in socket.scope && call.system in message[1]) {
                                    if (socket.scope[call.system].includes(call.talkgroup)) {
                                        if (message[1][call.system] && message[1][call.system][call.talkgroup]) {
                                            socket.send(JSON.stringify([wsCommand.call, call]));
                                        }
                                    }
                                }

                            } else {
                                this.eventEmitter.removeListener('call', socket.liveFeed);

                                socket.liveFeed = undefined;
                            }
                        };

                        this.eventEmitter.addListener('call', socket.liveFeed);

                        returnStatus = true;
                    }

                } else {
                    if (typeof socket.liveFeed === 'function') {
                        this.eventEmitter.removeListener('call', socket.liveFeed);
                    }

                    socket.liveFeed = undefined;

                    returnStatus = false;
                }

                socket.send(JSON.stringify([wsCommand.liveFeedMap, returnStatus]));

            } else if (message[0] === wsCommand.nop) {
                socket.send(JSON.stringify([wsCommand.nop]));

            } else if (message[0] === wsCommand.pin) {
                const token = Buffer.from(message[1], 'base64').toString();

                if (typeof socket.authCount !== 'number') {
                    socket.authCount = 1;

                } else {
                    socket.authCount += 1;
                }

                if (socket.authCount > maxAuthenticationTries) {
                    socket.send(JSON.stringify([wsCommand.pin]));

                } else {
                    socket.isAuthenticated = this.authenticate(token);

                    if (socket.isAuthenticated) {
                        socket.authCount = 0;

                    } else if (socket.authCount === maxAuthenticationTries) {
                        console.log(`Authentication: token=${token} Locked`);

                    } else {
                        console.log(`Authentication: token=${token} Failed`);
                    }

                    socket.scope = this.getScope(token);

                    if (socket.isAuthenticated) {
                        socket.send(JSON.stringify([wsCommand.config, await this.getConfig(socket.scope)]));


                    } else {
                        socket.send(JSON.stringify([wsCommand.pin]));
                    }
                }
            }
        }
    }

    validateApiKey(apiKey, system, talkgroup) {
        const scope = this.getScope(apiKey, this.app.config.apiKeys);

        return Array.isArray(scope[system]) && scope[system].includes(talkgroup);
    }
}

module.exports = Controller;
