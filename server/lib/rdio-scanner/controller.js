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

import { spawn, spawnSync } from 'child_process';
import EventEmitter from 'events';
import Sequelize from 'sequelize';

import { defaults } from './defaults.js';
import { Log } from './log.js';
import { version } from './version.js';
import { WebSocket } from './websocket.js';

const maxAuthenticationTries = 3;

const wsCommand = {
    call: 'CAL',
    config: 'CFG',
    expired: 'XPR',
    listCall: 'LCL',
    livefeedMap: 'LFM',
    max: 'MAX',
    pin: 'PIN',
    ver: 'VER',
};

export class Controller extends EventEmitter {
    get isAccessRestricted() {
        return !!this.config.access.length;
    }

    constructor(ctx) {
        super();

        this.setMaxListeners(0);

        this.config = ctx.config;

        this.config.on('config', () => {
            this.groupsMap = this.getGroupsMap();

            this.tagsMap = this.getTagsMap();

            this.broadcastConfig();

            this.pruneScheduler();
        });

        this.ffmpeg = !spawnSync('ffmpeg', ['-version']).error;

        this.groupsMap = this.getGroupsMap();

        this.log = ctx.log;

        this.models = ctx.models;

        this.pruneInterval = null;

        this.tagsMap = this.getTagsMap();

        this.version = version;

        this.websocket = null;

        if (!this.ffmpeg) {
            this.log.write(Log.warn, 'Controller: ffmpeg is missing, no audio conversion possible.');
        }

        this.pruneScheduler();
    }

    broadcastConfig() {
        if (this.websocket) {
            this.websocket.wss.clients.forEach(async (socket) => {
                if (this.isAccessRestricted && !socket.access) {
                    return;
                }

                socket.send(JSON.stringify([wsCommand.config, this.getConfig(socket.scope)]));
            });
        }
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

    getAccess(token) {
        const parse = (obj) => {
            if (Array.isArray(obj)) {
                return obj.find((o) => parse(o));

            } else if (obj !== null && typeof obj === 'object') {
                return obj.code === token ? obj : null;

            } else {
                return obj === token ? obj : null;
            }
        };

        return parse(this.config.access);
    }

    getAccessScope(token) {
        const parse = (record) => {
            const parseSystem = (system, first = true) => {
                const parseTalkgroup = (talkgroup, first = true) => {
                    if (Array.isArray(talkgroup)) {
                        return talkgroup.map((tg) => parseTalkgroup(tg, false));

                    } else if (talkgroup !== null && typeof talkgroup === 'object' && typeof talkgroup.id === 'number') {
                        return first ? [talkgroup.id] : talkgroup.id;

                    } else if (typeof talkgroup === 'number') {
                        return first ? [talkgroup] : talkgroup;

                    } else if (talkgroup === '*') {
                        const talkgroups = (this.config.systems.find((sys) => sys.id === system.id) || {}).talkgroups;

                        return parseTalkgroup(talkgroups);

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
                        const parsed = parseSystem(this.config.systems.find((sys) => sys.id === system), false);

                        return first ? [parsed] : parsed;

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
                return parse(record.find((acc) => acc === token || acc.code === token));

            } else if (
                record !== null && typeof record === 'object' &&
                (record.code === token || record.key === token) &&
                (typeof record.disabled !== 'boolean' || !record.disabled)
            ) {
                return parseSystem(record.systems);

            } else if (record === token) {
                return parseSystem(this.config.systems);

            } else {
                return [];
            }
        };

        return parse(this.config.access).reduce((obj, arr) => {
            obj[arr[0]] = arr[1];

            return obj;
        }, {});
    }

    getApiKeyScope(token, systems = this.config.systems) {
        const parse = (record) => {
            const parseSystem = (system, first = true) => {
                const parseTalkgroup = (talkgroup, first = true) => {
                    if (Array.isArray(talkgroup)) {
                        return talkgroup.map((tg) => parseTalkgroup(tg, false));

                    } else if (talkgroup !== null && typeof talkgroup === 'object' && typeof talkgroup.id === 'number') {
                        return first ? [talkgroup.id] : talkgroup.id;

                    } else if (typeof talkgroup === 'number') {
                        return first ? [talkgroup] : talkgroup;

                    } else if (talkgroup === '*') {
                        const talkgroups = (systems.find((sys) => sys.id === system.id) || {}).talkgroups;

                        return parseTalkgroup(talkgroups);

                    } else {
                        return [];
                    }
                };

                if (Array.isArray(system)) {
                    return system.map((sys) => parseSystem(sys, false)).filter((sys) => sys.length);

                } else if (system !== null && typeof system === 'object' && typeof system.id === 'number') {
                    if (systems.find((sys) => sys.id === system.id)) {
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
                    if (systems.find((sys) => sys.id === system)) {
                        const parsed = parseSystem(this.config.systems.find((sys) => sys.id === system), false);

                        return first ? [parsed] : parsed;

                    } else {
                        return [];
                    }

                } else if (system === '*') {
                    return parseSystem(systems);

                } else {
                    return [];
                }
            };

            if (record === null || record === undefined) {
                return [];

            } else if (Array.isArray(record)) {
                return parse(record.find((acc) => acc === token || acc.key === token));

            } else if (
                record !== null && typeof record === 'object' &&
                (record.code === token || record.key === token) &&
                (typeof record.disabled !== 'boolean' || !record.disabled)
            ) {
                return parseSystem(record.systems);

            } else if (record === token) {
                return parseSystem(systems);

            } else {
                return [];
            }
        };

        return parse(this.config.apiKeys).reduce((obj, arr) => {
            obj[arr[0]] = arr[1];

            return obj;
        }, {});
    }

    async getCall(id, scope) {
        const where = scope !== null && typeof scope === 'object' ? {
            [Sequelize.Op.and]: [
                { id },
                {
                    [Sequelize.Op.or]: Object.keys(scope).map((sys) => ({
                        system: sys,
                        talkgroup: { [Sequelize.Op.in]: scope[sys] },
                    }), []),
                },
            ],
        } : { id };

        const call = await this.models.call.findOne({ where });

        return call ? call.get() : null;
    }

    async getCalls(options, scope) {
        const filters = [];

        if (scope !== null && typeof scope === 'object') {
            filters.push({
                [Sequelize.Op.or]: Object.keys(scope).map((sys) => ({
                    system: +sys,
                    talkgroup: { [Sequelize.Op.in]: scope[sys] },
                })),
            });
        }

        if (options && typeof options.group === 'string' && options.group.length) {
            const group = this.groupsMap[options.group];

            filters.push({
                [Sequelize.Op.or]: Object.keys(group).map((sys) => ({
                    system: +sys,
                    talkgroup: { [Sequelize.Op.in]: group[sys] },
                })),
            });
        }

        if (options && typeof options.system === 'number') {
            filters.push({ system: options.system });
        }

        if (options && typeof options.tag === 'string' && options.tag.length) {
            const tag = this.tagsMap[options.tag];

            filters.push({
                [Sequelize.Op.or]: Object.keys(tag).map((sys) => ({
                    system: +sys,
                    talkgroup: { [Sequelize.Op.in]: tag[sys] },
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

        const where1 = filters.length ? { [Sequelize.Op.and]: filters } : {};

        const where2 = date ? {
            [Sequelize.Op.and]: [
                where1,
                {
                    dateTime: {
                        [Sequelize.Op.gte]: new Date(date.getFullYear(), date.getMonth(), date.getDate(), 0, 0, 0),
                        [Sequelize.Op.lte]: new Date(date.getFullYear(), date.getMonth(), date.getDate(), 23, 59, 59),
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
            return Object.keys(this.groupsMap).reduce((groups, group) => {
                Object.keys(this.groupsMap[group]).forEach((system) => {
                    if (Object.keys(scope).includes(system)) {
                        const talkgroups = this.groupsMap[group][system].filter((talkgroup) => scope[system].includes(talkgroup));

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
            return this.groupsMap;
        }
    }

    getGroupsMap(systems = this.config.systems) {
        const groupsMap = {};

        systems.forEach((system) => {
            system.talkgroups.forEach((talkgroup) => {
                const group = this.config.groups.find((group) => group._id === talkgroup.groupId);

                if (!group) {
                    return;
                }

                if (!groupsMap[group.label]) {
                    groupsMap[group.label] = {};
                }

                if (!groupsMap[group.label][system.id]) {
                    groupsMap[group.label][system.id] = [];
                }

                if (!groupsMap[group.label][system.id].includes(talkgroup.id)) {
                    groupsMap[group.label][system.id].push(talkgroup.id);
                }
            });
        });

        return groupsMap;
    }

    getOptions() {
        const dimmerDelay = this.config.options.dimmerDelay;

        const keypadBeeps = this.config.options.keypadBeeps === false ? false
            : this.config.options.keypadBeeps === 'uniden' ? defaults.keypadBeeps.uniden
                : this.config.options.keypadBeeps === 'whistler' ? defaults.keypadBeeps.whistler
                    : defaults.keypadBeeps.uniden;

        return { dimmerDelay, keypadBeeps };
    }

    getSystems(scope) {
        if (scope !== null && typeof scope === 'object') {
            const sysIds = Object.keys(scope).map((id) => +id);

            return this.config.systems.filter((sys) => sysIds.includes(sys.id)).map((sys) => ({
                id: sys.id,
                label: sys.label,
                led: sys.led,
                order: sys.order,
                talkgroups: sys.talkgroups.filter((tg) => scope[sys.id].includes(tg.id)).map((tg) => ({
                    group: (this.config.groups.find((g) => g._id === tg.groupId) || {}).label || 'Unknown',
                    id: tg.id,
                    label: tg.label,
                    led: tg.led,
                    name: tg.name,
                    tag: (this.config.tags.find((t) => t._id === tg.tagId) || {}).label || 'Untagged',
                })),
                units: sys.units.map((unit) => ({
                    id: unit.id,
                    label: unit.label,
                })),
            }));

        } else {
            return this.config.systems;
        }
    }

    getTags(scope) {
        if (scope !== null && typeof scope === 'object') {
            return Object.keys(this.tagsMap).reduce((tags, tag) => {
                Object.keys(this.tagsMap[tag]).forEach((system) => {
                    if (Object.keys(scope).includes(system)) {
                        const talkgroups = this.tagsMap[tag][system].filter((talkgroup) => scope[system].includes(talkgroup));

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
            return this.tagsMap;
        }
    }

    getTagsMap(systems = this.config.systems) {
        const tagsMap = {};

        systems.forEach((system) => {
            system.talkgroups.forEach((talkgroup) => {
                const tag = this.config.tags.find((tag) => tag._id === talkgroup.tagId);

                if (!tag) {
                    return;
                }

                if (!tagsMap[tag.label]) {
                    tagsMap[tag.label] = {};
                }

                if (!tagsMap[tag.label][system.id]) {
                    tagsMap[tag.label][system.id] = [];
                }

                if (!tagsMap[tag.label][system.id].includes(talkgroup.id)) {
                    tagsMap[tag.label][system.id].push(talkgroup.id);
                }
            });
        });

        return tagsMap;
    }

    async importCall(call = {}, meta) {
        meta = meta !== null && typeof meta === 'object' ? meta : {};

        const groups = this.config.groups;
        const systems = this.config.systems;
        const tags = this.config.tags;

        let system = systems.find((sys) => sys.id === call.system);

        let talkgroup = Array.isArray(system?.talkgroups) ? system.talkgroups.find((tg) => {
            if (tg.id === call.talkgroup) {
                return true;

            } else if ((tg.patches || '').split(',').map((id) => +id).includes(call.talkgroup)) {
                call.talkgroup = tg.id;

                return true;

            } else {
                return false;
            }
        }) : null;


        if (Array.isArray(system?.blacklists) && system.blacklists.includes(call.talkgroup)) {
            this.log.write(Log.info, [
                `NewCall: system=${call.system}`,
                `talkgroup=${call.talkgroup}`,
                `file=${call.audioName} Blacklisted`,
            ].join(' '));

            return;
        }

        let populated = false;

        if (this.config.options.autoPopulate && !system && call.system) {
            populated = true;

            system = {
                id: call.system,
                label: meta.systemLabel || `System ${call.system}`,
                talkgroups: [],
                units: [],
            };

            systems.push(system);
            systems.sort((a, b) => a.id - b.id);
        }

        if ((this.config.options.autoPopulate || system?.autoPopulate) && !talkgroup && call.talkgroup) {
            populated = true;

            talkgroup = {
                group: 'Unknown',
                id: call.talkgroup,
                label: meta.talkgroupLabel || `${call.talkgroup}`,
                name: `Talkgroup ${call.talkgroup}`,
                tag: 'Untagged'
            };

            let group = groups.find((group) => group.label == talkgroup.group);

            if (!group) {
                group = {
                    _id: groups.reduce((id, group) => group._id > id ? group._id : id, 0) + 1,
                    label: talkgroup.group,
                };
                groups.push(group);
                groups.sort((a, b) => a.label.localeCompare(b.label));
            }

            let tag = tags.find((tag) => tag.label == talkgroup.tag);

            if (!tag) {
                tag = {
                    _id: tags.reduce((id, tag) => tag._id > id ? tag._id : id, 0) + 1,
                    label: talkgroup.tag,
                };
                tags.push(tag);
                tags.sort((a, b) => a.label.localeCompare(b.label));
            }

            talkgroup.groupId = group._id;
            talkgroup.tagId = tag._id;

            system.talkgroups.push(talkgroup);

            if (this.config.options.sortTalkgroups) {
                system.talkgroups.sort((a, b) => a.id - b.id);
            }
        }

        if (populated) {
            this.config.groups = groups;
            this.config.systems = systems;
            this.config.tags = tags;
        }

        if (!system || !talkgroup) {
            this.log.write(Log.info, [
                `NewCall: system=${call.system || 'unknown'}`,
                `talkgroup=${call.talkgroup || 'unknown'}`,
                `file=${call.audioName} No matching system/talkgroup`,
            ].join(' '));

            return;
        }

        if (!this.config.options.disableDuplicateDetection) {
            const dateFrom = new Date(call.dateTime);
            const dateTo = new Date(call.dateTime);
            const delay = this.config.options.duplicateDetectionTimeFrame ?? defaults.options.duplicateDetectionTimeFrame;

            dateFrom.setMilliseconds(dateFrom.getMilliseconds() - delay);
            dateTo.setMilliseconds(dateTo.getMilliseconds() + delay);

            const duplicateCall = await this.models.call.findOne({
                where: {
                    dateTime: {
                        [Sequelize.Op.gte]: dateFrom,
                        [Sequelize.Op.lte]: dateTo,
                    },
                    system: call.system,
                    talkgroup: call.talkgroup,
                },
            });

            if (duplicateCall) {
                this.log.write(Log.warn, [
                    `NewCall: system=${call.system} talkgroup=${call.talkgroup}`,
                    `file=${call.audioName} Duplicate call rejected`,
                ].join(' '));

                return;
            }
        }

        if (!this.config.options.disableAudioConversion) {
            try {
                call = await this.convertCallAudio(call);

            } catch (error) {
                this.log.write(Log.error, [
                    `NewCall: system=${call.system} talkgroup=${call.talkgroup}`,
                    `file=${call.audioName} ${error.message}`,
                ].join(' '));
            }
        }

        let newCall;

        newCall = await this.models.call.create(call);

        this.log.write(Log.info, `NewCall: system=${call.system} talkgroup=${call.talkgroup} file=${call.audioName} Success`);

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
                socket.scope = this.getAccessScope();
            }

            if (message[0] === wsCommand.ver) {
                socket.send(JSON.stringify([wsCommand.ver, this.version]));

            } else if (this.isAccessRestricted && !socket.access && message[0] !== wsCommand.pin) {
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
                const token = Buffer.from(message[1], 'base64').toString();

                if (typeof socket.authCount !== 'number') {
                    socket.authCount = 1;

                } else {
                    socket.authCount += 1;
                }

                if (socket.authCount > maxAuthenticationTries) {
                    socket.send(JSON.stringify([wsCommand.pin]));

                } else if (this.isAccessRestricted) {
                    socket.access = this.getAccess(token);

                    if (socket.access) {
                        if (socket.access.expiration instanceof Date && socket.access.expiration.getTime() < new Date().getTime()) {
                            this.log.write(Log.warn, `Authentication: ident="${socket.access.ident}" expired`);

                            socket.send(JSON.stringify([wsCommand.expired]));

                            return;
                        }

                        socket.authCount = 0;

                        if (typeof socket.access.limit === 'number') {
                            const count = Array.from(this.websocket.wss.clients)
                                .reduce((c, s) => s.access?.code === token ? ++c : c, 0);

                            if (count > socket.access.limit) {
                                socket.send(JSON.stringify([wsCommand.max]));

                                this.log.write(Log.warn, `Authentication: ident="${socket.access.ident}" ` +
                                    `too many concurrent connections, limit is ${socket.access.limit}`);

                                return;
                            }
                        }

                    } else if (socket.authCount === maxAuthenticationTries) {
                        this.log.write(Log.warn, `Authentication: token="${token}" Locked`);

                    } else {
                        this.log.write(Log.warn, `Authentication: token="${token}" Failed`);
                    }

                    if (socket.access) {
                        socket.scope = this.getAccessScope(socket.access.code);

                        socket.send(JSON.stringify([wsCommand.config, this.getConfig(socket.scope)]));

                    } else {
                        socket.send(JSON.stringify([wsCommand.pin]));
                    }

                } else {
                    socket.send(JSON.stringify([wsCommand.config, this.getConfig(socket.scope)]));
                }
            }
        }
    }

    registerWebSocket(websocket) {
        if (websocket instanceof WebSocket) {
            this.websocket = websocket;
        }
    }

    pruneScheduler() {
        if (this.pruneInterval) {
            clearInterval(this.pruneInterval);
        }

        if (this.config.options.pruneDays > 0) {
            this.pruneInterval = setInterval(() => {
                const now = new Date();

                const olderThan = new Date(now.getFullYear(), now.getMonth(), now.getDate() - this.config.options.pruneDays);

                this.models.call.destroy({
                    where: {
                        dateTime: {
                            [Sequelize.Op.lt]: olderThan,
                        },
                    },
                });

                this.models.log.destroy({
                    where: {
                        dateTime: {
                            [Sequelize.Op.lt]: olderThan,
                        },
                    },
                });
            }, 15 * 60 * 1000);

        } else {
            this.pruneInterval = null;
        }
    }

    validateApiKey(apiKey, system, talkgroup) {
        const systems = (this.config.options.autoPopulate || this.config.systems.find((sys) => sys.id === system)?.autoPopulate)
            ? [{ id: system, talkgroups: [talkgroup] }]
            : this.config.systems;

        const scope = this.getApiKeyScope(apiKey, systems);

        return Array.isArray(scope[system]) && (
            scope[system].includes(talkgroup) || this.config.systems.some((sys) => {
                return sys.id === system && sys.talkgroups.some((tg) => {
                    return Array.isArray(tg.patches) && tg.patches.includes(talkgroup);
                });
            })
        );
    }
}
