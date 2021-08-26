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

class RdioScannerApiKey extends Sequelize.Model { }

export function apiKeyFactory(ctx) {
    RdioScannerApiKey.init(apiKeyFactory.schema, {
        modelName: 'rdioScannerApiKey',
        sequelize: ctx.sequelize,
        timestamps: false,
    });

    return RdioScannerApiKey;
}

apiKeyFactory.schema = {
    _id: {
        type: Sequelize.DataTypes.INTEGER,
        primaryKey: true,
        autoIncrement: true,
    },
    disabled: {
        type: Sequelize.DataTypes.BOOLEAN,
        defaultValue: false,
    },
    ident: {
        type: Sequelize.DataTypes.STRING,
        allowNull: true,
    },
    key: {
        type: Sequelize.DataTypes.STRING,
        allowNull: false,
        unique: true,
    },
    order: {
        type: Sequelize.DataTypes.INTEGER,
        allowNull: true,
    },
    systems: {
        type: Sequelize.DataTypes.TEXT,
        allowNull: false,
        get() {
            const rawValue = this.getDataValue('systems');
            try {
                return JSON.parse(rawValue);
            } catch (_) {
                return rawValue;
            }
        },
        set(value) {
            const rawValue = JSON.stringify(value);
            this.setDataValue('systems', rawValue);
        },
    },
};
