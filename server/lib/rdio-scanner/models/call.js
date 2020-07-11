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

const { DataTypes, Model } = require('sequelize');

class RdioScannerCall extends Model { }

function rdioScannerCallFactory(ctx = {}) {
    RdioScannerCall.init(rdioScannerCallFactory.schema, {
        modelName: 'rdioScannerCall',
        sequelize: ctx.sequelize,
        timestamps: false,
    });

    return RdioScannerCall;
}

rdioScannerCallFactory.schema = {
    id: {
        type: DataTypes.INTEGER,
        primaryKey: true,
        autoIncrement: true,
    },
    audio: {
        type: DataTypes.BLOB('long'),
        allowNull: false,
    },
    audioName: {
        type: DataTypes.STRING,
        allowNull: true,
    },
    audioType: {
        type: DataTypes.STRING,
        allowNull: true,
    },
    dateTime: {
        type: DataTypes.DATE,
        allowNull: false,
    },
    frequencies: {
        type: DataTypes.JSON,
        defaultValue: [],
        allowNull: false,
    },
    frequency: {
        type: DataTypes.INTEGER,
        allowNull: true,
    },
    source: {
        type: DataTypes.INTEGER,
        allownull: true,
    },
    sources: {
        type: DataTypes.JSON,
        defaultValue: [],
        allowNull: false,
    },
    system: {
        type: DataTypes.INTEGER,
        allowNull: false,
    },
    talkgroup: {
        type: DataTypes.INTEGER,
        allowNull: false,
    },
};

module.exports = rdioScannerCallFactory;
