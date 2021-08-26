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

class RdioScannerSystem extends Sequelize.Model { }

export function systemFactory(ctx) {
    RdioScannerSystem.init(systemFactory.schema, {
        modelName: 'rdioScannerSystem',
        sequelize: ctx.sequelize,
        timestamps: false,
    });

    return RdioScannerSystem;
}

systemFactory.schema = {
    _id: {
        type: Sequelize.DataTypes.INTEGER,
        primaryKey: true,
        autoIncrement: true,
    },
    autoPopulate: {
        type: Sequelize.DataTypes.BOOLEAN,
        defaultValue: false,
    },
    blacklists: {
        type: Sequelize.DataTypes.TEXT('long'),
        defaultValue: [],
        allowNull: false,
        get() {
            const rawValue = this.getDataValue('blacklists');
            try {
                return JSON.parse(rawValue);
            } catch (_) {
                return rawValue;
            }
        },
        set(value) {
            const rawValue = JSON.stringify(value);
            this.setDataValue('blacklists', rawValue);
        },
    },
    id: {
        type: Sequelize.DataTypes.INTEGER,
        allowNull: false,
        unique: true,
    },
    label: {
        type: Sequelize.DataTypes.STRING,
        allowNull: false,
    },
    led: {
        type: Sequelize.DataTypes.STRING,
        allowNull: true,
    },
    order: {
        type: Sequelize.DataTypes.INTEGER,
        allowNull: true,
    },
    talkgroups: {
        type: Sequelize.DataTypes.TEXT('long'),
        defaultValue: [],
        allowNull: false,
        get() {
            const rawValue = this.getDataValue('talkgroups');
            try {
                return JSON.parse(rawValue);
            } catch (_) {
                return rawValue;
            }
        },
        set(value) {
            const rawValue = JSON.stringify(value);
            this.setDataValue('talkgroups', rawValue);
        },
    },
    units: {
        type: Sequelize.DataTypes.TEXT('long'),
        defaultValue: [],
        allowNull: false,
        get() {
            const rawValue = this.getDataValue('units');
            try {
                return JSON.parse(rawValue);
            } catch (_) {
                return rawValue;
            }
        },
        set(value) {
            const rawValue = JSON.stringify(value);
            this.setDataValue('units', rawValue);
        },
    },
};
