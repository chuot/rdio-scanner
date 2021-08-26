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

class RdioScannerAccess extends Sequelize.Model { }

export function accessFactory(ctx) {
    RdioScannerAccess.init(accessFactory.schema, {
        modelName: 'rdioScannerAccess',
        sequelize: ctx.sequelize,
        timestamps: false,
    });

    return RdioScannerAccess;
}

accessFactory.schema = {
    _id: {
        type: Sequelize.DataTypes.INTEGER,
        primaryKey: true,
        autoIncrement: true,
    },
    code: {
        type: Sequelize.DataTypes.STRING,
        allowNull: false,
        unique: true,
    },
    expiration: {
        type: Sequelize.DataTypes.DATE,
        allowNull: true,
    },
    ident: {
        type: Sequelize.DataTypes.STRING,
        allowNull: true,
    },
    limit: {
        type: Sequelize.DataTypes.INTEGER,
        allowNull: true,
    },
    order: {
        type: Sequelize.DataTypes.INTEGER,
        allowNull: true,
    },
    systems: {
        type: Sequelize.DataTypes.TEXT('long'),
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
