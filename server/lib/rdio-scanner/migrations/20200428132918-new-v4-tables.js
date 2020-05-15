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

const dotenv = require('dotenv');
const fs = require('fs');
const path = require('path');
const uuid = require('uuid');

const rdioScannerCallFactory = require('../models/call');

module.exports = {

    up: async (queryInterface, Sequelize) => {
        const configFile = path.resolve(__dirname, '../../../config.json');

        const sequelize = queryInterface.sequelize;

        if (!fs.existsSync(configFile)) {

            const envFile = path.resolve(__dirname, '../../../.env');

            const envFileExists = fs.existsSync(envFile);

            const env = envFileExists ? dotenv.parse(fs.readFileSync(envFile)) : {};

            const oldSystem = sequelize.define('rdioScannerSystem', {
                aliases: {
                    type: Sequelize.JSON,
                    defaultValue: [],
                    allowNull: false,
                },
                name: {
                    type: Sequelize.STRING,
                    allowNull: false,
                },
                system: {
                    type: Sequelize.INTEGER,
                    allowNull: false,
                },
                talkgroups: {
                    type: Sequelize.JSON,
                    defaultValue: [],
                    allowNull: false,
                    get() {
                        let talkgroups = this.getDataValue('talkgroups');

                        if (typeof talkgroups === 'string') {
                            try {
                                talkgroups = JSON.parse(talkgroups);

                            } catch (error) {
                                talkgroups = [];
                            }
                        }

                        return talkgroups;
                    },
                },
            });

            let systems;

            if (envFileExists) {
                try {
                    systems = await oldSystem.findAll({ order: [['system', 'ASC']] });

                    systems = systems.map((s) => ({
                        id: s.system,
                        label: s.name,
                        talkgroups: s.talkgroups.map((t) => ({
                            id: t.dec,
                            label: t.alphaTag,
                            name: t.description,
                            tag: t.tag,
                            group: t.group,
                        })),
                        units: Array.isArray(s.aliases) ? s.aliases.map((a) => ({
                            id: a.uid,
                            label: a.name,
                        })) : [],
                    }));

                } catch (_) {
                    systems = getExampleSystems();
                }

            } else {
                systems = getExampleSystems();
            }

            const config = {
                nodejs: {
                    env: env.NODE_ENV || 'production',
                    host: env.NODE_HOST || '0.0.0.0',
                    port: env.NODE_PORT || 3000,
                },
                sequelize: {
                    database: env.DB_NAME || null,
                    dialect: env.DB_DIALECT || 'sqlite',
                    host: env.DB_HOST || null,
                    password: env.DB_PASS || null,
                    port: env.DB_PORT || null,
                    storage: env.DB_STORAGE || 'database.sqlite',
                    username: env.DB_USER || null,
                },
                rdioScanner: {
                    access: null,
                    allowDownload: env.RDIO_ALLOW_DOWNLOAD ? /true/i.exec(`${env.RDIO_ALLOW_DOWLOAD}`) : true,
                    apiKeys: env.RDIO_APIKEYS ? JSON.parse(env.RDIO_APIKEYS) : [uuid.v4()],
                    pruneDays: env.RDIO_PRUNE_DAYS ? parseInt(env.RDIO_PRUNE_DAYS, 10) : 7,
                    systems,
                    useDimmer: false,
                    useGroup: env.RDIO_USE_GROUP ? /true/i.exec(`${env.RDIO_USE_GROUP}`) : true,
                    useLed: true,
                },
            };

            fs.writeFileSync(configFile, JSON.stringify(config, null, 4));

            if (fs.existsSync(envFile)) {
                fs.unlinkSync(envFile);
            }
        }

        const transaction = await sequelize.transaction();

        try {
            rdioScannerCallFactory({ sequelize });

            await queryInterface.dropTable('rdioScannerSystems', { transaction });

            await queryInterface.createTable('rdioScannerCalls2', rdioScannerCallFactory.Schema, { transaction });

            await sequelize.query('INSERT INTO `rdioScannerCalls2` SELECT `id`,`audio`,`audioName`,`audioType`,`startTime`,`freqList`,`freq`,null,`srcList`,`system`,`talkgroup` FROM `rdioScannerCalls`', { transaction });

            await queryInterface.dropTable('rdioScannerCalls', { transaction });

            await queryInterface.renameTable('rdioScannerCalls2', 'rdioScannerCalls', { transaction });

            await queryInterface.addIndex('rdioScannerCalls', ['dateTime', 'system', 'talkgroup'], { transaction });

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

function getExampleSystems() {
    return [{
        id: 11,
        label: 'RSP25MTL1',
        talkgroups: [
            {
                id: 54241,
                label: 'TDB A1',
                name: 'MRC TDB Fire Alpha 1',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 54242,
                label: 'TDB B1',
                name: 'MRC TDB Fire Bravo 1',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 54243,
                label: 'TDB B2',
                name: 'MRC TDB Fire Bravo 2',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 54248,
                label: 'TDB B3',
                name: 'MRC TDB Fire Bravo 3',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 54251,
                label: 'TDB B4',
                name: 'MRC TDB Fire Bravo 4',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 54261,
                label: 'TDB B5',
                name: 'MRC TDB Fire Bravo 5',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 54244,
                label: 'TDB B6',
                name: 'MRC TDB Fire Bravo 6',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 54129,
                label: 'TDB B7',
                name: 'MRC TDB Fire Bravo 7',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 54125,
                label: 'TDB B8',
                name: 'MRC TDB Fire Bravo 8',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
        ],
        units: [
            {
                id: 4424001,
                label: 'CAUCA',
            },
        ],
    },
    {
        id: 21,
        label: 'SERAM',
        talkgroups: [
            {
                id: 60040,
                label: 'GENERAL',
                name: 'SERAM General',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 60041,
                label: 'REPART',
                name: 'SERAM Repartition',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50001,
                label: 'SG 1',
                name: 'SERAM Regroupement 1',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50002,
                label: 'SG 2',
                name: 'SERAM Regroupement 2',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50003,
                label: 'SG 3',
                name: 'SERAM Regroupement 3',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50004,
                label: 'SG 4',
                name: 'SERAM Regroupement 4',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50005,
                label: 'SG 5',
                name: 'SERAM Regroupement 5',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50006,
                label: 'SG 6',
                name: 'SERAM Regroupement 6',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50007,
                label: 'SG 7',
                name: 'SERAM Regroupement 7',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 50008,
                label: 'SG 8',
                name: 'SERAM Regroupement 8',
                tag: 'Fire Dispatch',
                group: 'FIRE DISPATCH',
            },
            {
                id: 60051,
                label: 'CMD 3',
                name: 'SERAM Commandement 3',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60052,
                label: 'CMD 4',
                name: 'SERAM Commandement 4',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60053,
                label: 'CMD 5',
                name: 'SERAM Commandement 5',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60054,
                label: 'CMD 6',
                name: 'SERAM Commandement 6',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60055,
                label: 'CMD 7',
                name: 'SERAM Commandement 7',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60056,
                label: 'CMD 8',
                name: 'SERAM Commandement 8',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60059,
                label: 'CMD 11',
                name: 'SERAM Commandement 11',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60091,
                label: 'CMD 12',
                name: 'SERAM Commandement 12',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60092,
                label: 'CMD 13',
                name: 'SERAM Commandement 13',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60057,
                label: 'CMD 14',
                name: 'SERAM Commandement 14',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60061,
                label: 'OPS 3',
                name: 'SERAM Operations 3',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60062,
                label: 'OPS 4',
                name: 'SERAM Operations 4',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60063,
                label: 'OPS 5',
                name: 'SERAM Operations 5',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60064,
                label: 'OPS 6',
                name: 'SERAM Operations 6',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60065,
                label: 'OPS 7',
                name: 'SERAM Operations 7',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60066,
                label: 'OPS 8',
                name: 'SERAM Operations 8',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60069,
                label: 'OPS 11',
                name: 'SERAM Operations 11',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60093,
                label: 'OPS 12',
                name: 'SERAM Operations 12',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60094,
                label: 'OPS 13',
                name: 'SERAM Operations 13',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
            {
                id: 60067,
                label: 'OPS 14',
                name: 'SERAM Operations 14',
                tag: 'Fire Tac',
                group: 'FIRE TAC',
            },
        ],
        units: [{
            id: 702099,
            label: 'DISPATCH',
        }],
    }];
}
