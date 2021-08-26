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

import Sequelize from 'sequelize';

class RdioScannerCall extends Sequelize.Model { }

export function callFactory(ctx) {
    RdioScannerCall.init(callFactory.schema, {
        modelName: 'rdioScannerCall',
        sequelize: ctx.sequelize,
        timestamps: false,
    });

    return RdioScannerCall;
}

callFactory.schema = {
    id: {
        type: Sequelize.DataTypes.INTEGER,
        primaryKey: true,
        autoIncrement: true,
    },
    audio: {
        type: Sequelize.DataTypes.BLOB('long'),
        allowNull: false,
    },
    audioName: {
        type: Sequelize.DataTypes.STRING,
        allowNull: true,
    },
    audioType: {
        type: Sequelize.DataTypes.STRING,
        allowNull: true,
    },
    dateTime: {
        type: Sequelize.DataTypes.DATE,
        allowNull: false,
    },
    frequencies: {
        type: Sequelize.DataTypes.TEXT('long'),
        defaultValue: '[]',
        allowNull: false,
        get() {
            const rawValue = this.getDataValue('frequencies');
            try {
                return JSON.parse(rawValue);
            } catch (_) {
                return rawValue;
            }
        },
        set(value) {
            const rawValue = JSON.stringify(value);
            this.setDataValue('frequencies', rawValue);
        },
    },
    frequency: {
        type: Sequelize.DataTypes.INTEGER,
        allowNull: true,
    },
    source: {
        type: Sequelize.DataTypes.INTEGER,
        allownull: true,
    },
    sources: {
        type: Sequelize.DataTypes.TEXT('long'),
        defaultValue: '[]',
        allowNull: false,
        get() {
            const rawValue = this.getDataValue('sources');
            try {
                return JSON.parse(rawValue);
            } catch (_) {
                return rawValue;
            }
        },
        set(value) {
            const rawValue = JSON.stringify(value);
            this.setDataValue('sources', rawValue);
        },
    },
    system: {
        type: Sequelize.DataTypes.INTEGER,
        allowNull: false,
    },
    talkgroup: {
        type: Sequelize.DataTypes.INTEGER,
        allowNull: false,
    },
};
