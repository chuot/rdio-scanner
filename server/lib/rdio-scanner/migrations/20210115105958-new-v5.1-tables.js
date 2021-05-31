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

import bcrypt from 'bcrypt';
import crypto from 'crypto';
import fs from 'fs';
import path from 'path';
import url from 'url';

import { Config } from '../config.js';
import { defaults } from '../defaults.js';
import { accessFactory } from '../models/access.js';
import { apiKeyFactory } from '../models/api-key.js';
import { callFactory } from '../models/call.js';
import { configFactory } from '../models/config.js';
import { dirWatchFactory } from '../models/dir-watch.js';
import { downstreamFactory } from '../models/downstream.js';
import { groupFactory } from '../models/group.js';
import { logFactory } from '../models/log.js';
import { systemFactory } from '../models/system.js';
import { tagFactory } from '../models/tag.js';

const dirname = path.dirname(url.fileURLToPath(import.meta.url));

const configFile = path.resolve(process.env.APP_DATA || path.join(dirname, '../../..'), 'config.json');

export default {

    up: async (queryInterface) => {
        let oldConfig;

        if (fs.existsSync(configFile)) {
            try {
                oldConfig = JSON.parse(fs.readFileSync(configFile));

            } catch (error) {
                console.error(error.message);
            }
        }

        if (oldConfig === null || typeof oldConfig !== 'object') {
            oldConfig = {};
        }

        if (oldConfig.rdioScanner === null || typeof oldConfig.rdioScanner !== 'object') {
            oldConfig.rdioScanner = {};
        }

        if (!Array.isArray(oldConfig.rdioScanner.systems)) {
            oldConfig.rdioScanner.systems = [];
        }

        const accesses = parseAccess(oldConfig.rdioScanner.access);

        const apiKeys = parseApiKeys(oldConfig.rdioScanner.apiKeys);

        const dirWatch = parseDirWatch(oldConfig.rdioScanner.dirWatch);

        const downstreams = parseDownstreams(oldConfig.rdioScanner.downstreams);

        const options = parseOptions(oldConfig.rdioScanner.options);

        const groups = [];

        const tags = [];

        let systems = oldConfig.rdioScanner.systems.map((system) => {
            system.talkgroups = system.talkgroups.map((talkgroup) => {
                if (talkgroup.group && !groups.find((group) => group.label === talkgroup.group)) {
                    groups.push({ _id: groups.length + 1, label: talkgroup.group });
                }

                if (talkgroup.tag && !tags.find((tag) => tag.label === talkgroup.tag)) {
                    tags.push({ _id: tags.length + 1, label: talkgroup.tag });
                }

                const group = groups.find((g) => g.label === talkgroup.group);
                const tag = tags.find((t) => t.label === talkgroup.tag);

                talkgroup.groupId = group ? group._id : null;
                talkgroup.tagId = tag ? tag._id : null;

                talkgroup.patches = Array.isArray(talkgroup.patches) ? talkgroup.patches.join(',') : '';

                delete talkgroup.group;
                delete talkgroup.tag;

                return talkgroup;
            });

            return system;
        });

        systems = parseSystems(oldConfig.rdioScanner.systems);

        const transaction = await queryInterface.sequelize.transaction();

        try {
            await queryInterface.createTable('rdioScannerAccesses', accessFactory.schema, { transaction });

            if (accesses.length) {
                await queryInterface.bulkInsert('rdioScannerAccesses', accesses.map((access) => {
                    access.systems = JSON.stringify(access.systems);

                    return access;
                }), { transaction });
            }

            await queryInterface.createTable('rdioScannerApiKeys', apiKeyFactory.schema, { transaction });

            if (apiKeys.length) {
                await queryInterface.bulkInsert('rdioScannerApiKeys', apiKeys.map((apiKey) => {
                    apiKey.systems = JSON.stringify(apiKey.systems);

                    return apiKey;
                }), { transaction });
            }

            await queryInterface.createTable('rdioScannerCalls2', callFactory.schema, { transaction });

            await queryInterface.addIndex('rdioScannerCalls2', ['dateTime', 'system', 'talkgroup'], { transaction });

            await queryInterface.sequelize.query([
                'INSERT INTO `rdioScannerCalls2`',
                'SELECT `id`,`audio`,`audioName`,`audioType`,`dateTime`,`frequencies`,`frequency`,`source`,`sources`,`system`,`talkgroup`',
                'FROM `rdioScannerCalls`',
            ].join(' '), { transaction });

            await queryInterface.dropTable('rdioScannerCalls', { transaction });

            await queryInterface.renameTable('rdioScannerCalls2', 'rdioScannerCalls', { transaction });

            await queryInterface.createTable('rdioScannerConfigs', configFactory.schema, { transaction });

            await queryInterface.addIndex('rdioScannerConfigs', ['key'], { transaction });

            await queryInterface.bulkInsert('rdioScannerConfigs', [
                { key: Config.adminPassword, val: JSON.stringify(bcrypt.hashSync(defaults.adminPassword, 10)) },
                { key: Config.adminPasswordNeedChange, val: JSON.stringify(true) },
                { key: Config.options, val: JSON.stringify(options) },
                { key: Config.secret, val: JSON.stringify(crypto.randomBytes(128).toString('hex')) },
            ], { transaction });

            await queryInterface.createTable('rdioScannerDirWatches', dirWatchFactory.schema, { transaction });

            if (dirWatch.length) {
                await queryInterface.bulkInsert('rdioScannerDirWatches', dirWatch, { transaction });
            }

            await queryInterface.createTable('rdioScannerDownstreams', downstreamFactory.schema, { transaction });

            if (downstreams.length) {
                await queryInterface.bulkInsert('rdioScannerDownstreams', downstreams.map((downstream) => {
                    downstream.systems = JSON.stringify(downstream.systems);

                    return downstream;
                }), { transaction });
            }

            await queryInterface.createTable('rdioScannerGroups', groupFactory.schema, { transaction });

            if (groups.length) {
                await queryInterface.bulkInsert('rdioScannerGroups', groups, { transaction });
            }

            await queryInterface.createTable('rdioScannerLogs', logFactory.schema, { transaction });

            await queryInterface.addIndex('rdioScannerLogs', ['dateTime', 'level'], { transaction });

            await queryInterface.createTable('rdioScannerSystems', systemFactory.schema, { transaction });

            if (systems.length) {
                await queryInterface.bulkInsert('rdioScannerSystems', parseSystems(systems).slice().map((system) => {
                    system.blacklists = JSON.stringify([]);
                    system.talkgroups = JSON.stringify(system.talkgroups);
                    system.units = JSON.stringify(system.units);

                    return system;
                }), { transaction });
            }

            await queryInterface.createTable('rdioScannerTags', tagFactory.schema, { transaction });

            if (tags.length) {
                await queryInterface.bulkInsert('rdioScannerTags', tags, { transaction });
            }

            await transaction.commit();

        } catch (err) {
            await transaction.rollback();

            throw err;
        }
    },

    down: async () => {
        console.log('No rollback possible.');
    },
};

function parseAccess(access) {
    if (access === null || access === undefined) {
        access = [];

    } else if (typeof access === 'string') {
        access = [{ code: access, systems: '*' }];

    } else if (typeof access === 'object' && !Array.isArray(access)) {
        access = [access];
    }

    return access.map((acc, index) => {
        const ident = acc[Object.keys(acc).filter((k) => !['code', 'systems'].includes(k))] || 'Unknown';
        const order = index + 1;

        let code;
        let systems;

        if (typeof acc === 'string') {
            code = acc;
            systems = '*';

        } else {
            code = typeof acc.code === 'string' && acc.code.length ? acc.code : null;
            systems = rewriteSystemsProperty(acc.systems);
        }

        return { code, ident, order, systems };
    }).filter((acc, idx, arr) => acc.code !== null && arr.findIndex((v) => v.code === acc.code) === idx);
}

function parseApiKeys(apiKeys) {
    if (apiKeys === null || apiKeys === undefined) {
        apiKeys = [];

    } else if (typeof apiKeys === 'string') {
        apiKeys = [{ key: apiKeys, systems: '*' }];

    } else if (typeof apiKeys === 'object' && !Array.isArray(apiKeys)) {
        apiKeys = [apiKeys];
    }

    return apiKeys.map((api, index) => {
        let key;
        let systems;

        if (typeof api === 'string') {
            key = api;
            systems = '*';

        } else {
            key = typeof api.key === 'string' && api.key.length ? api.key : null;
            systems = rewriteSystemsProperty(api.systems);
        }

        return {
            disabled: typeof api.disabled === 'boolean' ? api.disabled : false,
            ident: api[Object.keys(api).filter((k) => !['key', 'systems'].includes(k))] || 'Unknown',
            key,
            order: index + 1,
            systems,
        };
    }).filter((api, idx, arr) => api.key !== null && arr.findIndex((v) => v.key === api.key) === idx);
}

function parseDirWatch(dirWatch) {
    dirWatch = Array.isArray(dirWatch) ? dirWatch : [];

    return dirWatch.map((dw, index) => ({
        delay: typeof dw.delay === 'number' ? dw.delay : defaults.dirWatch.delay,
        deleteAfter: typeof dw.deleteAfter === 'boolean' ? dw.deleteAfter : defaults.dirWatch.deleteAfter,
        directory: typeof dw.directory === 'string' && dw.directory.length ? dw.directory : null,
        disabled: typeof dw.disabled === 'boolean' ? dw.disabled : false,
        extension: typeof dw.extension === 'string' && dw.extension.length ? dw.extension : null,
        frequency: typeof dw.frequency === 'number' ? dw.frequency : null,
        mask: typeof dw.mask === 'string' ? dw.mask : null,
        order: index + 1,
        systemId: typeof dw.system === 'number' ? dw.system : null,
        talkgroupId: typeof dw.talkgroup === 'number' ? dw.talkgroup : null,
        type: ['sdr-trunk', 'trunk-recorder'].includes(dw.type) ? dw.type : null,
        usePolling: typeof dw.usePolling === 'boolean' ? dw.usePolling
            : typeof dw.usePolling === 'number' ? true : defaults.dirWatch.usePolling,
    })).filter((dw, idx, arr) => dw.directory !== null && dw.system !== null && arr.findIndex((v) => v.directory === dw.directory) === idx);
}

function parseDownstreams(downstreams) {
    if (downstreams === null || downstreams === undefined) {
        downstreams = [];

    } else if (typeof downstreams === 'string') {
        downstreams = [{ key: downstreams, systems: '*' }];

    } else if (typeof downstreams === 'object' && !Array.isArray(downstreams)) {
        downstreams = [downstreams];
    }

    return downstreams.map((ds, index) => {
        if (ds === null || ds === undefined) {
            return { apiKey: null };
        }

        return {
            apiKey: typeof ds.apiKey === 'string' ? ds.apiKey : null,
            disabled: typeof ds.disabled === 'boolean' ? ds.disabled : false,
            order: index + 1,
            systems: rewriteSystemsProperty(ds.systems, true),
            url: typeof ds.url === 'string' ? ds.url : null,
        };
    }).filter((ds, idx, arr) => !(ds.apiKey === null || ds.url === null) && arr.findIndex((v) => v.apiKey === ds.apiKey) === idx);
}

function parseOptions(options) {
    options = options !== null && typeof options === 'object' ? options : {};

    return {
        autoPopulate: typeof options.autoPopulate === 'boolean' ? options.autoPopulate : defaults.options.autoPopulate,
        dimmerDelay: typeof options.dimmerDelay === 'number' ? options.dimmerDelay : defaults.options.dimmerDelay,
        disableAudioConversion: typeof options.disableAudioConversion === 'boolean'
            ? options.disableAudioConversion : defaults.options.disableAudioConversion,
        disableDuplicateDetection: typeof options.disableDuplicateDetection === 'boolean'
            ? options.disableDuplicateDetection : defaults.options.disableDuplicateDetection,
        keypadBeeps: options.keypadBeeps === 1 ? 'uniden' : options.keypadBeeps === 2 ? 'whistler' : 'uniden',
        pruneDays: typeof options.pruneDays === 'number' ? options.pruneDays : defaults.options.pruneDays,
        sortTalkgroups: typeof options.sortTalkgroups === 'boolean' ? options.sortTalkgroups : defaults.options.sortTalkgroups,
    };
}

function parseSystems(systems) {
    systems = Array.isArray(systems) ? systems : [];

    return systems.map((sys) => ({
        _id: typeof sys._id === 'number' ? sys._id : null,
        autoPopulate: typeof sys.autoPopulate === 'boolean' ? sys.autoPopulate : false,
        id: typeof sys.id === 'number' ? sys.id : null,
        label: typeof sys.label === 'string' ? sys.label : 'Unknown',
        led: typeof sys.led === 'string' ? sys.led : null,
        order: typeof sys.order === 'number' ? sys.order : null,
        talkgroups: Array.isArray(sys.talkgroups) ? sys.talkgroups.map((tg) => ({
            frequency: typeof tg.frequency === 'number' ? tg.frequency : null,
            groupId: typeof tg.groupId === 'number' ? tg.groupId : null,
            id: typeof tg.id === 'number' ? tg.id : null,
            label: typeof tg.label === 'string' ? tg.label : `${tg.id}`,
            led: typeof tg.led === 'string' ? tg.led : null,
            name: typeof tg.name === 'string' ? tg.name : `Talkgroup ${tg.id}`,
            patches: typeof tg.patches === 'string' ? tg.patches : '',
            tagId: typeof tg.tagId === 'number' ? tg.tagId : null,
        })).filter((tg) => tg.id !== null) : [],
        units: Array.isArray(sys.units) ? sys.units.map((unit) => ({
            id: typeof unit.id === 'number' ? unit.id : null,
            label: typeof unit.label === 'string' ? unit.label : `${unit.id}`,
        })).filter((unit) => unit.id !== null) : [],
    })).filter((system, idx, arr) => system.id !== null && arr.indexOf(system) === idx).sort((sysA, sysB) => {
        if (typeof sysA.order === 'number' && typeof sysB.order !== 'number') {
            return -1;

        } else if (typeof sysA.order !== 'number' && typeof sysB.order === 'number') {
            return 1;

        } else if (typeof sysA.order === 'number' && typeof sysB.order === 'number') {
            return sysA.order - sysB.order;

        } else {
            return sysA.id - sysB.id;
        }
    });
}

function rewriteSystemsProperty(systems, id_as = false) {
    if (systems === '*') {
        return systems;
    }

    return (systems !== null && systems !== undefined ? Array.isArray(systems) ? systems : [systems] : []).map((sys) => {
        if (sys === null || sys === undefined) {
            sys = { id: null };

        } else if (typeof sys === 'object') {
            const parsed = {
                id: typeof sys.id === 'number' ? sys.id : null,
                talkgroups: rewriteTalkgroupsProperty(sys.talkgroups, id_as),
            };

            if (id_as && typeof sys.id_as === 'number') {
                parsed.id_as = sys.id_as;
            }

            sys = Object.keys(parsed).sort().reduce((s, k) => {
                s[k] = parsed[k];

                return s;
            }, {});
        }

        return sys;
    }).filter((sys) => sys.id !== null);
}

function rewriteTalkgroupsProperty(talkgroups, id_as = false) {
    if (talkgroups === '*') {
        return talkgroups;
    }

    return (talkgroups !== null && talkgroups !== undefined ? Array.isArray(talkgroups) ? talkgroups : [talkgroups] : []).map((tg) => {
        if (tg === null || tg === undefined) {
            tg = { id: null };

        } else if (typeof tg === 'object') {
            const parsed = { id: typeof tg.id === 'number' ? tg.id : null };

            if (id_as && typeof tg.id_as === 'number') {
                parsed.id_as = tg.id_as;
            }

            tg = parsed;
        }

        return tg;
    }).filter((tg) => tg.id !== null);
}
