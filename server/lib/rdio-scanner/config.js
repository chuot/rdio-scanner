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
import EventEmitter from 'events';
import Sequelize from 'sequelize';

import { defaults } from './defaults.js';

export class Config extends EventEmitter {
    get access() {
        return this._access.slice();
    }

    set access(access) {
        access = Array.isArray(access) ? access : [];

        this._access = access.map((val) => ({
            _id: typeof val._id === 'number' ? val._id : null,
            code: typeof val.code === 'string' && val.code.length ? val.code : null,
            expiration: val.expiration instanceof Date ? val.expiration : null,
            ident: typeof val.ident === 'string' && val.ident.length ? val.ident : null,
            limit: typeof val.limit === 'number' ? val.limit : null,
            order: typeof val.order === 'number' ? val.order : null,
            systems: val.systems || null,
        })).filter((val) => val.code && val.systems).sort(sortByOrder);

        this.emit('changes');
    }

    get adminPassword() {
        return this._adminPassword;
    }

    set adminPassword(adminPassword) {
        if (typeof adminPassword === 'string' && adminPassword.length) {
            if (adminPassword.indexOf('$2b') !== 0) {
                adminPassword = bcrypt.hashSync(adminPassword, 10);
            }

            this._adminPassword = adminPassword;

            this._adminPasswordNeedChange = bcrypt.compareSync(defaults.adminPassword, this.adminPassword);

            this.emit('changes');
        }
    }

    get adminPasswordNeedChange() {
        return this._adminPasswordNeedChange;
    }

    set adminPasswordNeedChange(adminPasswordNeedChange) {
        if (typeof adminPasswordNeedChange === 'boolean') {
            this._adminPasswordNeedChange = adminPasswordNeedChange;

            this.emit('changes');
        }
    }

    get apiKeys() {
        return this._apiKeys.slice();
    }

    set apiKeys(apiKeys) {
        apiKeys = Array.isArray(apiKeys) ? apiKeys : [];

        this._apiKeys = apiKeys.map((val) => ({
            _id: typeof val._id === 'number' ? val._id : null,
            disabled: typeof val.disabled === 'boolean' ? val.disabled : false,
            ident: typeof val.ident === 'string' && val.ident.length ? val.ident : null,
            key: typeof val.key === 'string' && val.key.length ? val.key : null,
            order: typeof val.order === 'number' ? val.order : null,
            systems: val.systems || null,
        })).filter((val) => val.key && val.systems).sort(sortByOrder);

        this.emit('changes');
    }

    get dirWatch() {
        return this._dirWatch.slice();
    }

    set dirWatch(dirWatch) {
        dirWatch = Array.isArray(dirWatch) ? dirWatch : [];

        this._dirWatch = dirWatch.map((val) => ({
            _id: typeof val._id === 'number' ? val._id : null,
            delay: typeof val.delay === 'number' ? val.delay : defaults.dirWatch.delay,
            deleteAfter: typeof val.deleteAfter === 'boolean' ? val.deleteAfter : defaults.dirWatch.deleteAfter,
            directory: typeof val.directory === 'string' && val.directory.length ? val.directory : null,
            disabled: typeof val.disabled === 'boolean' ? val.disabled : false,
            extension: typeof val.extension === 'string' && val.extension.length ? val.extension : null,
            frequency: typeof val.frequency === 'number' ? val.frequency : null,
            mask: typeof val.mask === 'string' && val.mask.length ? val.mask : null,
            order: typeof val.order === 'number' ? val.order : null,
            systemId: typeof val.systemId === 'number' ? val.systemId : null,
            talkgroupId: typeof val.talkgroupId === 'number' ? val.talkgroupId : null,
            type: typeof val.type === 'string' && val.type.length ? val.type : null,
            usePolling: typeof val.usePolling === 'boolean' ? val.usePolling : defaults.dirWatch.usePolling,
        })).filter((val) => val.directory).sort(sortByOrder);

        this.emit('changes');
    }

    get downstreams() {
        return this._downstreams.slice();
    }

    set downstreams(downstreams) {
        downstreams = Array.isArray(downstreams) ? downstreams : [];

        this._downstreams = downstreams.map((val) => ({
            _id: typeof val._id === 'number' ? val._id : null,
            apiKey: typeof val.apiKey === 'string' && val.apiKey.length ? val.apiKey : null,
            disabled: typeof val.disabled === 'boolean' ? val.disabled : false,
            order: typeof val.order === 'number' ? val.order : null,
            systems: val.systems || null,
            url: typeof val.url === 'string' && val.url.length ? val.url : null,
        })).filter((val) => val.apiKey && val.url).sort(sortByOrder);

        this.emit('changes');
    }

    get groups() {
        return this._groups.slice();
    }

    set groups(groups) {
        groups = Array.isArray(groups) ? groups : [];

        this._groups = groups.map((val) => ({
            _id: typeof val._id === 'number' ? val._id : null,
            label: typeof val.label === 'string' && val.label.length ? val.label : null,
        })).filter((val) => val.label).sort(sortByLabel);

        this.emit('changes');
    }

    get options() {
        return Object.assign({}, this._options);
    }

    set options(options) {
        if (options !== null && !Array.isArray(options) && typeof options === 'object') {
            this._options = Object.keys(options).reduce((o, k) => {
                if ([
                    'autoPopulate',
                    'dimmerDelay',
                    'disableAudioConversion',
                    'disableDuplicateDetection',
                    'keypadBeeps',
                    'pruneDays',
                    'sortTalkgroups',
                ].includes(k)) {
                    o[k] = options[k];
                }

                return o;
            }, {});

            this.emit('changes');
        }
    }

    get secret() {
        return this._secret;
    }

    get systems() {
        return this._systems.slice();
    }

    set systems(systems) {
        systems = Array.isArray(systems) ? systems : [];

        this._systems = systems.map((sys) => ({
            _id: typeof sys._id === 'number' ? sys._id : null,
            autoPopulate: typeof sys.autoPopulate === 'boolean' ? sys.autoPopulate : false,
            blacklists: (Array.isArray(sys.blacklists) ? sys.blacklists : [])
                .filter((bl) => typeof bl === 'number')
                .filter((bl, idx, arr) => arr.indexOf(bl) === idx)
                .sort(),
            id: typeof sys.id === 'number' ? sys.id : null,
            label: typeof sys.label === 'string' && sys.label.length ? sys.label : `${sys.id}`,
            led: typeof sys.led === 'string' && sys.led.length ? sys.led : null,
            order: typeof sys.order === 'number' ? sys.order : null,
            talkgroups: (Array.isArray(sys.talkgroups) ? sys.talkgroups : [])
                .map((tg) => ({
                    frequency: tg.frequency,
                    groupId: typeof tg.groupId === 'number' ? tg.groupId : null,
                    id: typeof tg.id === 'number' ? tg.id : null,
                    label: typeof tg.label === 'string' && tg.label.length ? tg.label : null,
                    led: typeof tg.led === 'string' && tg.led.length ? tg.led : null,
                    name: typeof tg.name === 'string' && tg.name.length ? tg.name : `Talkgroup ${tg.id}`,
                    patches: typeof tg.patches === 'string' && tg.patches.length ? tg.patches : null,
                    tagId: typeof tg.tagId === 'number' ? tg.tagId : null,
                }))
                .filter((tg) => tg.id !== null && tg.groupId !== null && tg.tagId !== null),
            units: (Array.isArray(sys.units) ? sys.units : [])
                .map((unit) => ({
                    id: typeof unit.id === 'number' ? unit.id : null,
                    label: typeof unit.label === 'string' && unit.label.length ? unit.label : null,
                }))
                .filter((unit) => unit.id !== null),
        })).filter((sys) => sys.id !== null).sort(sortByOrder);

        this.emit('changes');
    }

    get tags() {
        return this._tags.slice();
    }

    set tags(tags) {
        tags = Array.isArray(tags) ? tags : [];

        this._tags = tags.map((val) => ({
            _id: typeof val._id === 'number' ? val._id : null,
            label: typeof val.label === 'string' && val.label.length ? val.label : null,
        })).filter((val) => val.label).sort(sortByLabel);

        this.emit('changes');
    }

    constructor(ctx) {
        super();

        this._log = ctx.log;
        this._models = ctx.models;
        this._sequelize = ctx.sequelize;

        this._access = [];
        this._adminPassword = '';
        this._adminPasswordNeedChange = false;
        this._apiKeys = [];
        this._dirWatch = [];
        this._downstreams = [];
        this._groups = [];
        this._options = defaults.options;
        this._secret = null;
        this._systems = [];
        this._tags = [];

        this._debounceTimeout = null;

        this.on('changes', () => {
            if (this._debounceTimeout !== null) {
                clearTimeout(this._debounceTimeout);
            }

            this._debounceTimeout = setTimeout(async () => {
                this._debounceTimeout = null;

                try {
                    writeConfig(this).then(() => {
                        this.emit('persisted');

                        readConfig(this).then(() => this.emit('config', {
                            access: this.access,
                            apiKeys: this.apiKeys,
                            dirWatch: this.dirWatch,
                            downstreams: this.downstreams,
                            groups: this.groups,
                            options: this.options,
                            systems: this.systems,
                            tags: this.tags,
                        }));
                    });

                } catch (error) {
                    console.error(`Config@write: ${error.message}`);
                }
            }, 1000);
        });

        try {
            readConfig(this).then(() => this.emit('ready'));

        } catch (error) {
            console.error(`Config@read: ${error.message}`);
        }
    }
}

Config.access = 'access';
Config.adminPassword = 'adminPassword';
Config.adminPasswordNeedChange = 'adminPasswordNeedChange';
Config.apiKeys = 'apiKeys';
Config.dirWatch = 'dirWatch';
Config.downstreams = 'downstreams';
Config.groups = 'groups';
Config.options = 'options';
Config.secret = 'secret';
Config.tags = 'tags';
Config.systems = 'systems';

async function readConfig(ctx) {
    if (!ctx) return;

    await Promise.all([
        ctx._models.access.findAll({ sort: [['order', 'ASC']] }),
        ctx._models.config.findOne({ where: { key: Config.adminPassword } }),
        ctx._models.config.findOne({ where: { key: Config.adminPasswordNeedChange } }),
        ctx._models.apiKey.findAll({ sort: [['order', 'ASC']] }),
        ctx._models.dirWatch.findAll({ sort: [['order', 'ASC']] }),
        ctx._models.downstream.findAll({ sort: [['order', 'ASC']] }),
        ctx._models.group.findAll(),
        ctx._models.config.findOne({ where: { key: Config.options } }),
        ctx._models.config.findOne({ where: { key: Config.secret } }),
        ctx._models.system.findAll(),
        ctx._models.tag.findAll(),
    ]).then(([
        access,
        adminPassword,
        adminPasswordNeedChange,
        apiKeys,
        dirWatch,
        downstreams,
        groups,
        options,
        secret,
        systems,
        tags,
    ]) => {
        ctx._access = access.map((model) => model.get());
        ctx._adminPassword = adminPassword.get('val');
        ctx._adminPasswordNeedChange = adminPasswordNeedChange.get('val');
        ctx._apiKeys = apiKeys.map((model) => model.get());
        ctx._dirWatch = dirWatch.map((model) => model.get());
        ctx._downstreams = downstreams.map((model) => model.get());
        ctx._groups = groups.map((model) => model.get());
        ctx._options = options.get('val');
        ctx._secret = secret.get('val');
        ctx._systems = systems.map((model) => model.get());
        ctx._tags = tags.map((model) => model.get());
    });
}

async function writeConfig(ctx) {
    if (!ctx) return;

    const transaction = await ctx._sequelize.transaction({ type: 'DEFERRED' });

    await ctx._models.config.upsert({
        key: Config.adminPassword,
        val: ctx.adminPassword,
    }, { transaction });

    await ctx._models.config.upsert({
        key: Config.adminPasswordNeedChange,
        val: ctx.adminPasswordNeedChange,
    }, { transaction });

    await ctx._models.config.upsert({
        key: Config.options,
        val: ctx.options,
    }, { transaction });

    await Promise.all([
        // access
        ctx._models.access.destroy({
            where: {
                _id: {
                    [Sequelize.Op.notIn]: ctx.access.map((o) => o._id).filter((o) => o !== null),
                },
            },
            transaction,
        }),
        ...ctx.access.map((access) => ctx._models.access.upsert(access, { transaction })),

        // apiKeys
        ctx._models.apiKey.destroy({
            where: {
                _id: {
                    [Sequelize.Op.notIn]: ctx.apiKeys.map((o) => o._id).filter((o) => o !== null),
                },
            },
            transaction,
        }),
        ...ctx.apiKeys.map((apiKey) => ctx._models.apiKey.upsert(apiKey, { transaction })),

        // dirWatch
        ctx._models.dirWatch.destroy({
            where: {
                _id: {
                    [Sequelize.Op.notIn]: ctx.dirWatch.map((o) => o._id).filter((o) => o !== null),
                },
            },
            transaction,
        }),
        ...ctx.dirWatch.map((dirWatch) => ctx._models.dirWatch.upsert(dirWatch, { transaction })),

        // downstream
        ctx._models.downstream.destroy({
            where: {
                _id: {
                    [Sequelize.Op.notIn]: ctx.downstreams.map((o) => o._id).filter((o) => o !== null),
                },
            },
            transaction,
        }),
        ...ctx.downstreams.map((downstream) => ctx._models.downstream.upsert(downstream, { transaction })),

        // groups
        ctx._models.group.destroy({
            where: {
                _id: {
                    [Sequelize.Op.notIn]: ctx.groups.map((o) => o._id).filter((o) => o !== null),
                },
            },
            transaction,
        }),
        ...ctx.groups.map((group) => ctx._models.group.upsert(group, { transaction })),

        // system
        ctx._models.system.destroy({
            where: {
                _id: {
                    [Sequelize.Op.notIn]: ctx.systems.map((o) => o._id).filter((o) => o !== null),
                },
            },
            transaction,
        }),
        ...ctx.systems.map((system) => ctx._models.system.upsert(system, { transaction })),

        // tags
        ctx._models.tag.destroy({
            where: {
                _id: {
                    [Sequelize.Op.notIn]: ctx.tags.map((o) => o._id).filter((o) => o !== null),
                },
            },
            transaction,
        }),
        ...ctx.tags.map((tag) => ctx._models.tag.upsert(tag, { transaction })),
    ]);

    await transaction.commit();
}

function sortByLabel() {
    return (a, b) => typeof a.label === 'string' && typeof b.label === 'string'
        ? a.label.localeCompare(b.label) : 0;
}

function sortByOrder() {
    return (a, b) => {
        if (typeof a.order === 'number' && typeof b.order !== 'number') {
            return -1;

        } else if (typeof a.order !== 'number' && typeof b.order === 'number') {
            return 1;

        } else if (typeof a.order === 'number' && typeof b.order === 'number') {
            return a.order - b.order;

        } else {
            return 0;
        }
    };
}