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

const { spawn, spawnSync } = require('child_process');

const EventEmitter = require('events');

const { Op } = require('sequelize');

const uuid = require('uuid');

const defaults = require('./defaults');

const WebSocket = require('./websocket');

const maxAuthenticationTries = 3;

const wsCommand = {
    call: 'CAL',
    config: 'CFG',
    listCall: 'LCL',
    livefeedMap: 'LFM',
    pin: 'PIN',
};

class Controller extends EventEmitter {
    get isAccessRestricted() {
        return this.config.access !== null;
    }

    constructor(ctx = {}) {
        super();

        this.setMaxListeners(0);

        this.app = ctx.app;

        this.config = parseConfig(ctx.config);

        this.groups = {};

        this.models = ctx.models;

        this.tags = {};

        this.websocket = null;

        this.ffmpeg = !spawnSync('ffmpeg', ['-version']).error;

        if (!this.ffmpeg) {
            console.warn('ffmpeg is missing, no audio conversion possible.');
        }

        if (this.config.options.pruneDays > 0) {
            this.pruneInterval = setInterval(() => {
                const now = new Date();

                this.models.call.destroy({
                    where: {
                        dateTime: {
                            [Op.lt]: new Date(now.getFullYear(), now.getMonth(), now.getDate() - this.config.options.pruneDays),
                        },
                    },
                });
            }, 15 * 60 * 1000);
        }

        this.buildGroupsAndTags();
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

        return parse(this.config.access);
    }

    broadcastConfig() {
        if (this.websocket) {
            this.websocket.getSockets().forEach(async (socket) => {
                socket.scope = this.getScope(socket.token);

                socket.send(JSON.stringify([wsCommand.config, this.getConfig(socket.scope)]));
            })
        }
    }

    buildGroupsAndTags() {
        this.config.systems.forEach((system) => {
            system.talkgroups.forEach((talkgroup) => {
                if (!this.groups[talkgroup.group]) {
                    this.groups[talkgroup.group] = {};
                }

                if (!this.groups[talkgroup.group][system.id]) {
                    this.groups[talkgroup.group][system.id] = [];
                }

                if (!this.groups[talkgroup.group][system.id].includes(talkgroup.id)) {
                    this.groups[talkgroup.group][system.id].push(talkgroup.id);
                }

                if (!this.tags[talkgroup.tag]) {
                    this.tags[talkgroup.tag] = {};
                }

                if (!this.tags[talkgroup.tag][system.id]) {
                    this.tags[talkgroup.tag][system.id] = [];
                }

                if (!this.tags[talkgroup.tag][system.id].includes(talkgroup.id)) {
                    this.tags[talkgroup.tag][system.id].push(talkgroup.id);
                }
            })
        });
    }

    convertCallAudio(call = {}) {
        return new Promise((resolve, reject) => {
            if (!this.ffmpeg) {
                reject('No ffmpeg available');

                return;
            }

            if (Buffer.isBuffer(call && call.audio)) {
                const metadata = [];

                const system = this.config.systems.find((sys) => sys.id === call.system);

                if (system && Array.isArray(system.talkgroups)) {
                    const talkgroup = system.talkgroups.find((tg) => tg.id === call.talkgroup);

                    if (talkgroup) {
                        metadata.push(...[
                            '-metadata', `album="${talkgroup.label}"`,
                            '-metadata', `artist="${system.label}"`,
                            '-metadata', `date="${call.dateTime}"`,
                            '-metadata', `genre="${talkgroup.tag}"`,
                            '-metadata', `title="${talkgroup.name}"`,
                        ]);
                    }
                }

                const proc = spawn('ffmpeg', [
                    '-i',
                    '-',
                    '-af',
                    'loudnorm=I=-16:dual_mono=true:TP=-1.5:LRA=11',
                    ...metadata,
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

                proc.on('error', (error) => reject(error.message));

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

        const call = await this.models.call.findOne({ where });

        return call || null;
    }

    async getCalls(options, scope) {
        const filters = [];

        if (scope !== null && typeof scope === 'object') {
            filters.push({
                [Op.or]: Object.keys(scope).map((sys) => ({
                    system: +sys,
                    talkgroup: { [Op.in]: scope[sys] },
                })),
            });
        }

        if (options && typeof options.group === 'string' && options.group.length) {
            const group = this.groups[options.group];

            filters.push({
                [Op.or]: Object.keys(group).map((sys) => ({
                    system: +sys,
                    talkgroup: { [Op.in]: group[sys] },
                })),
            });
        }

        if (options && typeof options.system === 'number') {
            filters.push({ system: options.system });
        }

        if (options && typeof options.tag === 'string' && options.tag.length) {
            const tag = this.tags[options.tag];

            filters.push({
                [Op.or]: Object.keys(tag).map((sys) => ({
                    system: +sys,
                    talkgroup: { [Op.in]: tag[sys] },
                })),
            });
        }

        if (options && typeof options.talkgroup === 'number') {
            filters.push({ talkgroup: options.talkgroup });
        }

        const attributes = ['id', 'dateTime', 'system', 'talkgroup'];

        const date = options && typeof options.date === 'string' ? new Date(options.date) : null;

        const limit = options && typeof options.limit === 'number' ? Math.min(500, options.limit) : 500;

        const offset = options && typeof options.offset === 'number' ? options.offset : 0;

        const order = [['dateTime', options && typeof options.sort === 'number' && options.sort < 0 ? 'DESC' : 'ASC']];

        const where1 = filters.length ? { [Op.and]: filters } : {};

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
            await this.models.call.findOne({ attributes: ['dateTime'], order: [['dateTime', 'ASC']], where: where1 }),
            await this.models.call.findOne({ attributes: ['dateTime'], order: [['dateTime', 'DESC']], where: where1 }),
            await this.models.call.count({ where: where2 }),
            await this.models.call.findAll({ attributes, limit, offset, order, where: where2 }),
        ]);

        const dateStart = dateStartQuery && dateStartQuery.get('dateTime');

        const dateStop = dateStopQuery && dateStopQuery.get('dateTime');

        return { count, dateStart, dateStop, options, results };
    }

    getConfig(scope) {
        return Object.assign({}, this.getOptions(), {
            groups: this.getGroups(scope),
            systems: this.getSystems(scope),
            tags: this.getTags(scope),
        });
    }

    getGroups(scope) {
        if (scope !== null && typeof scope === 'object') {
            return Object.keys(this.groups).reduce((groups, group) => {
                Object.keys(this.groups[group]).forEach((system) => {
                    if (Object.keys(scope).includes(system)) {
                        const talkgroups = this.groups[group][system]
                            .filter((talkgroup) => scope[system].includes(talkgroup));

                        if (talkgroups.length) {
                            if (!groups[group]) {
                                groups[group] = {};
                            }

                            groups[group][system] = talkgroups;
                        }
                    }

                });

                return groups;
            }, {});

        } else {
            return this.groups;
        }
    }

    getOptions() {
        const options = this.config.options;

        const dimmerDelay = this.config.options.dimmerDelay;

        const keyboardShortcuts = !this.config.options.disableKeyboardShortcuts;

        const keypadBeeps = options.keypadBeeps === false ? false
            : options.keypadBeeps === 1 ? defaults.keypadBeeps.uniden
                : options.keypadBeeps === 2 ? defaults.keypadBeeps.whistler
                    : options.keypadBeeps !== null && typeof options.keypadBeeps === 'object' ? options.keypadBeeps
                        : defaults.keypadBeeps.uniden;

        return { dimmerDelay, keyboardShortcuts, keypadBeeps };
    }

    getScope(token, store = this.config.access) {
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
                    if (this.config.systems.find((sys) => sys.id === system.id)) {
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
                    if (this.config.systems.find((sys) => sys.id === system)) {
                        const systems = parseSystem(this.config.systems.find((sys) => sys.id === system), false);

                        return first ? [systems] : systems;

                    } else {
                        return [];
                    }

                } else if (system === '*') {
                    return parseSystem(this.config.systems);

                } else {
                    return [];
                }
            };

            if (record === null || record === undefined) {
                return this.isAccessRestricted ? [] : parseSystem(this.config.systems);

            } else if (Array.isArray(record)) {
                return parse(record.find((acc) => acc === token || acc.code === token || acc.key === token));

            } else if (record !== null && typeof record === 'object' && (record.code === token || record.key === token)) {
                const systems = parseSystem(record.systems);

                return first ? systems : systems;

            } else if (record === token) {
                return parseSystem(this.config.systems);

            } else {
                return [];
            }
        }

        return parse(store).reduce((obj, arr) => {
            obj[arr[0]] = arr[1];

            return obj;
        }, {});
    }

    getSystems(scope) {
        if (scope !== null && typeof scope === 'object') {
            const sysIds = Object.keys(scope).map((id) => +id);

            return this.config.systems.filter((sys) => sysIds.includes(sys.id));

        } else {
            return this.config.systems;
        }
    }

    getTags(scope) {
        if (scope !== null && typeof scope === 'object') {
            return Object.keys(this.tags).reduce((tags, tag) => {
                Object.keys(this.tags[tag]).forEach((system) => {
                    if (Object.keys(scope).includes(system)) {
                        const talkgroups = this.tags[tag][system]
                            .filter((talkgroup) => scope[system].includes(talkgroup));

                        if (talkgroups.length) {
                            if (!tags[tag]) {
                                tags[tag] = {};
                            }

                            tags[tag][system] = talkgroups;
                        }
                    }

                });

                return tags;
            }, {});

        } else {
            return this.tags;
        }
    }

    async importCall(call = {}, meta) {
        meta = meta !== null && typeof meta === 'object' ? meta : {};

        let system = this.config.systems.find((sys) => sys.id === call.system);

        let talkgroup = Array.isArray(system && system.talkgroups) ? system.talkgroups.find((tg) => {
            if (tg.id === call.talkgroup) {
                return true;

            } else if (Array.isArray(tg.patches) && tg.patches.includes(call.talkgroup)) {
                call.talkgroup = tg.id;

                return true;

            } else {
                return false;
            }
        }) : null;

        if (this.config.options.autoPopulate) {
            let populated = false;

            if (!system && call.system) {
                populated = true;

                system = {
                    id: call.system,
                    label: meta.systemLabel || `System ${call.system}`,
                    talkgroups: [],
                    units: [],
                };

                this.config.systems.push(system);

                this.config.systems.sort((a, b) => a.id - b.id);
            }

            if (!talkgroup && call.talkgroup) {
                populated = true;

                talkgroup = {
                    group: 'Unknown',
                    id: call.talkgroup,
                    label: `${call.talkgroup}`,
                    name: `Talkgroup ${call.talkgroup}`,
                    tag: 'untagged'
                };

                system.talkgroups.push(talkgroup);

                if (this.config.options.sortTalkgroups) {
                    system.talkgroups.sort((a, b) => a.id - b.id);
                }

                this.buildGroupsAndTags();
            }

            if (populated) {
                this.app.config.persist();

                this.broadcastConfig();
            }
        }

        if (!system || !talkgroup) {
            console.log(`NewCall: system=${call.system || 'unknown'} talkgroup=${call.talkgroup || 'unknown'} `
                + `file=${call.audioName} No matching system/talkgroup`);

            return;
        }

        const dateFrom = new Date(call.dateTime);
        const dateTo = new Date(call.dateTime);

        dateFrom.setMilliseconds(dateFrom.getMilliseconds() - 500);
        dateTo.setMilliseconds(dateTo.getMilliseconds() + 500);

        const duplicateCall = await this.models.call.findOne({
            where: {
                dateTime: {
                    [Op.gte]: dateFrom,
                    [Op.lte]: dateTo,
                },
                system: call.system,
                talkgroup: call.talkgroup,
            },
        });

        if (duplicateCall) {
            console.log(`NewCall: system=${call.system} talkgroup=${call.talkgroup} file=${call.audioName} Duplicate call rejected`);

            return;
        }

        if (!this.config.disableAudioConversion) {
            try {
                call = await this.convertCallAudio(call);

            } catch (error) {
                console.log(`NewCall: system=${call.system} talkgroup=${call.talkgroup} file=${call.audioName} ${error.message}`);
            }
        }

        let newCall;

        newCall = await this.models.call.create(call);

        console.log(`NewCall: system=${call.system} talkgroup=${call.talkgroup} file=${call.audioName} Success`);

        this.emit('call', newCall);
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
                socket.send(JSON.stringify([wsCommand.pin]));

            } else if (message[0] === wsCommand.call) {
                const call = await this.getCall(message[1], socket.scope);

                const response = [wsCommand.call, call];

                if (message[2]) {
                    response.push(message[2]);
                }

                socket.send(JSON.stringify(response));

            } else if (message[0] === wsCommand.config) {
                socket.send(JSON.stringify([wsCommand.config, this.getConfig(socket.scope)]));

            } else if (message[0] === wsCommand.listCall) {
                socket.send(JSON.stringify([wsCommand.listCall, await this.getCalls(message[1], socket.scope)]));

            } else if (message[0] === wsCommand.livefeedMap) {
                let returnStatus;

                if (message[1] !== null && typeof message[1] === 'object') {
                    const allOff = Object.keys(message[1]).every((sys) => Object.keys(message[1][sys]).every((tg) => !message[1][sys][tg]));

                    if (allOff) {
                        if (typeof socket.livefeed === 'function') {
                            this.removeListener('call', socket.livefeed);
                        }

                        socket.livefeed = undefined;

                        returnStatus = true;

                    } else {
                        if (typeof socket.livefeed === 'function') {
                            this.removeListener('call', socket.livefeed);
                        }

                        socket.livefeed = (call) => {
                            if (socket.readyState !== 3) {
                                if (call.system in socket.scope && call.system in message[1]) {
                                    if (socket.scope[call.system].includes(call.talkgroup)) {
                                        if (message[1][call.system] && message[1][call.system][call.talkgroup]) {
                                            socket.send(JSON.stringify([wsCommand.call, call]));
                                        }
                                    }
                                }

                            } else {
                                this.removeListener('call', socket.livefeed);

                                socket.livefeed = undefined;
                            }
                        };

                        this.addListener('call', socket.livefeed);

                        returnStatus = true;
                    }

                } else {
                    if (typeof socket.livefeed === 'function') {
                        this.removeListener('call', socket.livefeed);
                    }

                    socket.livefeed = undefined;

                    returnStatus = false;
                }

                socket.send(JSON.stringify([wsCommand.livefeedMap, returnStatus]));

            } else if (message[0] === wsCommand.pin) {
                socket.token = Buffer.from(message[1], 'base64').toString();

                if (typeof socket.authCount !== 'number') {
                    socket.authCount = 1;

                } else {
                    socket.authCount += 1;
                }

                if (socket.authCount > maxAuthenticationTries) {
                    socket.send(JSON.stringify([wsCommand.pin]));

                } else {
                    socket.isAuthenticated = this.authenticate(socket.token);

                    if (socket.isAuthenticated) {
                        socket.authCount = 0;

                    } else if (socket.authCount === maxAuthenticationTries) {
                        console.log(`Authentication: token=${socket.token} Locked`);

                    } else {
                        console.log(`Authentication: token=${socket.token} Failed`);
                    }

                    socket.scope = this.getScope(socket.token);

                    if (socket.isAuthenticated) {
                        socket.send(JSON.stringify([wsCommand.config, this.getConfig(socket.scope)]));

                    } else {
                        socket.send(JSON.stringify([wsCommand.pin]));
                    }
                }
            }
        }
    }

    registerWebSocket(websocket) {
        if (websocket instanceof WebSocket) {
            this.websocket = websocket;
        }
    }

    validateApiKey(apiKey, system, talkgroup) {
        if (typeof apiKey === 'string' && apiKey === this.config.apiKeys) {
            return true;

        } else if (Array.isArray(this.config.apiKeys) && this.config.apiKeys.includes(apiKey)) {
            return true;

        } else {
            const scope = this.getScope(apiKey, this.config.apiKeys);

            return Array.isArray(scope[system]) && (
                scope[system].includes(talkgroup) || this.config.systems.some((sys) => {
                    return sys.id === system && sys.talkgroups.some((tg) => {
                        return Array.isArray(tg.patches) && tg.patches.includes(talkgroup);
                    })
                })
            );
        }
    }
}

function parseConfig(config) {
    const ledColors = ['blue', 'cyan', 'green', 'magenta', 'red', 'white', 'yellow']

    const oscillatorNodeType = ['sine', 'square', 'sawtooth', 'triangle'];

    config.access = config.access !== undefined ? config.access : null;

    config.apiKeys = config.apiKeys !== undefined ? config.apiKeys : uuid.v4();

    if (config.options === null || typeof config.options !== 'object') {
        config.options = {};
    }

    if (config.options.allowDownload !== null || config.options.allowDownload !== undefined) {
        delete config.options.allowDownload;
    }

    if (config.allowDownload !== null || config.allowDownload !== undefined) {
        delete config.allowDownload;
    }

    config.options.autoPopulate = typeof config.options.autoPopulate === 'boolean' ? config.options.autoPopulate : true;

    config.options.dimmerDelay = typeof config.options.dimmerDelay === 'number' && config.options.dimmerDelay > 0
        ? config.options.dimmerDelay : 5000;

    config.options.disableAudioConversion = typeof config.options.disableAudioConversion === 'boolean'
        ? config.options.disableAudioConversion : typeof config.disableAudioConversion === 'boolean'
            ? config.disableAudioConversion : false;

    if (config.disableAudioConversion !== null || config.disableAudioConversion !== undefined) {
        delete config.disableAudioConversion;
    }

    config.options.disableKeyboardShortcuts = typeof config.options.disableKeyboardShortcuts === 'boolean'
        ? config.options.disableKeyboardShortcuts : false;

    config.options.keypadBeeps = config.options.keypadBeeps === false ? false
        : config.options.keyBeep === false ? false
            : config.options.keypadBeeps !== null && typeof config.options.keypadBeeps === 'object' ? config.options.keypadBeeps
                : [1, 2].includes(config.options.keypadBeeps) ? config.options.keypadBeeps
                    : 1;

    if (config.options.keyBeep !== undefined) {
        delete config.options.keyBeep;
    }

    if (typeof config.options.keypadBeeps === 'object') {
        if (Array.isArray(config.options.keypadBeeps.activate)) {
            config.options.keypadBeeps.activate.forEach((beep, index) => {
                beep.begin = typeof beep.begin === 'number' ? beep.begin : index * 0.05;
                beep.end = typeof beep.end === 'number' ? beep.end : index * 0.05 + 0.05;
                beep.frequency = typeof beep.frequency === 'number' ? beep.frequency : 1200;
                beep.type = oscillatorNodeType.includes(beep.type) ? beep.type : 'square';
            });

        } else {
            config.options.keypadBeeps.activate = [{
                begin: 0,
                end: 0.05,
                frequency: 1200,
                type: 'square',
            }];

        }

        if (Array.isArray(config.options.keypadBeeps.deactivate)) {
            config.options.keypadBeeps.deactivate.forEach((beep, index) => {
                beep.begin = typeof beep.begin === 'number' ? beep.begin : index * 0.1;
                beep.end = typeof beep.end === 'number' ? beep.end : index * 0.1 + 0.1;
                beep.frequency = typeof beep.frequency === 'number' ? beep.frequency : 925;
                beep.type = oscillatorNodeType.includes(beep.type) ? beep.type : 'square';
            });

        } else {
            config.options.keypadBeeps.deactivate = [{
                begin: 0,
                end: 0.1,
                frequency: 1200,
                type: 'square',
            }, {
                begin: 0.1,
                end: 0.2,
                frequency: 925,
                type: 'square',
            }];
        }

        if (Array.isArray(config.options.keypadBeeps.denied)) {
            config.options.keypadBeeps.denied.forEach((beep, index) => {
                beep.begin = typeof beep.begin === 'number' ? beep.begin : index * 0.1;
                beep.end = typeof beep.end === 'number' ? beep.end : index * 0.1 + 0.05;
                beep.frequency = typeof beep.frequency === 'number' ? beep.frequency : 925;
                beep.type = oscillatorNodeType.includes(beep.type) ? beep.type : 'square';
            });

        } else {
            config.options.keypadBeeps.denied = [{
                begin: 0,
                end: 0.05,
                frequency: 925,
                type: 'square',
            }, {
                begin: 0.1,
                end: 0.15,
                frequency: 925,
                type: 'square',
            }];
        }
    }

    config.options.pruneDays = typeof config.options.pruneDays === 'number' ? config.options.pruneDays
        : typeof config.pruneDays === 'number' ? config.pruneDays : 7;

    if (config.pruneDays !== null || config.pruneDays !== undefined) {
        delete config.pruneDays;
    }

    config.options.sortTalkgroups = typeof config.options.sortTalkgroups === 'boolean' ? config.options.sortTalkgroups : false;

    if (config.options.useDimmer !== undefined) {
        delete config.options.useDimmer;
    }

    if (config.useDimmer !== undefined) {
        delete config.useDimmer;
    }

    if (config.options.useGroup !== null || config.options.useGroup !== undefined) {
        delete config.options.useGroup;
    }

    if (config.useGroup !== null || config.useGroup !== undefined) {
        delete config.useGroup;
    }

    if (config.useLed !== null || config.useLed !== undefined) {
        delete config.useLed;
    }

    if (config.options.useLed !== null || config.options.useLed !== undefined) {
        delete config.options.useLed;
    }

    config.systems = Array.isArray(config.systems) ? config.systems : defaults.systems;

    config.systems.forEach((system) => {
        if (system === null || typeof system !== 'object') {
            console.error(`Config: Unknown system definition: ${JSON.stringify(system)}`);

            return;
        }

        if (typeof system.id !== 'number') {
            console.error(`Config: System.id ${JSON.stringify(system.id)} not a number`);
        }

        if (typeof system.label !== 'string' || !system.label.length) {
            console.error(`Config: System ${system.id}, label not a string: ${JSON.stringify(system.label)}`);
        }

        if (typeof system.led !== 'undefined') {
            system.led = ledColors.includes(system.led) ? system.led : 'green';
        }

        if (system.order !== undefined && typeof system.order !== 'number') {
            delete system.order;
        }

        if (!Array.isArray(system.talkgroups)) {
            system.talkgroups = [];
        }

        if (!system.talkgroups.length) {
            console.error(`Config: System ${system.id}, no talkgroups`);
        }

        system.talkgroups.forEach((talkgroup) => {
            if (talkgroup === null || typeof talkgroup !== 'object') {
                console.error(`Config: System ${system.id}, unknown talkgroup definition: ${JSON.stringify(talkgroup)}`);

                return;
            }

            if (typeof talkgroup.id !== 'number') {
                console.error(`Config: System ${system.id}, talkgroup.id not a number: ${JSON.stringify(talkgroup.id)}`);
            }

            if (typeof talkgroup.group !== 'string' || !talkgroup.group.length) {
                console.error(`Config: System ${system.id}, talkgroup ${talkgroup.id}, `
                    + `group not a string: ${JSON.stringify(talkgroup.group)}`);
            }

            if (typeof talkgroup.label !== 'string' || !talkgroup.label.length) {
                console.error(`Config: System ${system.id}, talkgroup ${talkgroup.id}, `
                    + `label not a string: ${JSON.stringify(talkgroup.label)}`);
            }

            if (typeof talkgroup.led !== 'undefined') {
                talkgroup.led = ledColors.includes(talkgroup.led) ? talkgroup.led : 'green';
            }

            if (typeof talkgroup.name !== 'string' || !talkgroup.name.length) {
                console.error(`Config: System ${system.id}, talkgroup ${talkgroup.id}, `
                    + `name not a string: ${JSON.stringify(talkgroup.name)}`);
            }

            if (talkgroup.patches !== undefined && !Array.isArray(talkgroup.patches)) {
                console.error(`Config: System ${system.id}, talkgroup ${talkgroup.id}, `
                    + `patches not an array: ${JSON.stringify(talkgroup.patches)}`);
            }

            if (typeof talkgroup.tag !== 'string' || !talkgroup.tag.length) {
                console.error(`Config: System ${system.id}, talkgroup ${talkgroup.id}, tag not a string: ${JSON.stringify(talkgroup.tag)}`);
            }
        });

        if (!Array.isArray(system.units)) {
            system.units = [];
        }

        system.units.forEach((unit) => {
            if (unit === null || typeof unit !== 'object') {
                console.error(`Config: System ${system.id}, unknown unit definition: ${JSON.stringify(unit)}`);

                return;
            }

            if (typeof unit.id !== 'number') {
                console.error(`Config: System ${system.id}, unit.id not a number: ${JSON.stringify(unit.id)}`);
            }

            if (typeof unit.label !== 'string' || !unit.label.length) {
                console.error(`Config: System ${system.id}, unit ${unit.id}, label not a string: ${JSON.stringify(unit.label)}`);
            }
        });
    });

    return config;
}

module.exports = Controller;
