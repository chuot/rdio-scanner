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

const { DataTypes, Model, Sequelize } = require('sequelize');

class RdioScannerSystem extends Model { }

function rdioScannerSystemFactory(app) {
    const db = new Sequelize({ dialect: 'sqlite', logging: false });

    RdioScannerSystem.init(rdioScannerSystemFactory.Schema, {
        modelName: 'rdioScannerSystem',
        sequelize: db,
        timestamps: false,
    });

    db.getQueryInterface()
        .createTable('rdioScannerSystems', rdioScannerSystemFactory.Schema)
        .then(() => RdioScannerSystem.bulkCreate(app.config.systems));

    return RdioScannerSystem;
}

rdioScannerSystemFactory.Schema = {
    id: {
        type: DataTypes.INTEGER,
        allowNull: false,
        primaryKey: true,
    },
    label: {
        type: DataTypes.STRING,
        allowNull: false,
    },
    led: {
        type: DataTypes.STRING,
        allowNull: true,
    },
    talkgroups: {
        type: DataTypes.JSON,
        defaultValue: [],
        allowNull: false,
    },
    units: {
        type: DataTypes.JSON,
        defaultValue: [],
        allowNull: false,
    },
};

module.exports = rdioScannerSystemFactory;
