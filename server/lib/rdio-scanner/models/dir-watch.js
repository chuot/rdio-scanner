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

class RdioScannerDirWatch extends Sequelize.Model { }

export function dirWatchFactory(ctx) {
    RdioScannerDirWatch.init(dirWatchFactory.schema, {
        modelName: 'rdioScannerDirWatch',
        sequelize: ctx.sequelize,
        timestamps: false,
    });

    return RdioScannerDirWatch;
}

dirWatchFactory.schema = {
    _id: {
        type: Sequelize.DataTypes.INTEGER,
        primaryKey: true,
        autoIncrement: true,
    },
    delay: {
        type: Sequelize.DataTypes.INTEGER,
        defaultValue: 0,
    },
    deleteAfter: {
        type: Sequelize.DataTypes.BOOLEAN,
        defaultValue: false,
    },
    directory: {
        type: Sequelize.DataTypes.STRING,
        allowNull: false,
        unique: true,
    },
    disabled: {
        type: Sequelize.DataTypes.BOOLEAN,
        defaultValue: false,
    },
    extension: {
        type: Sequelize.DataTypes.STRING,
        allowNull: true,
    },
    frequency: {
        type: Sequelize.DataTypes.INTEGER,
        allowNull: true,
    },
    mask: {
        type: Sequelize.DataTypes.STRING,
        allowNull: true,
    },
    order: {
        type: Sequelize.DataTypes.INTEGER,
        allowNull: true,
    },
    systemId: {
        type: Sequelize.DataTypes.INTEGER,
        allowNull: true,
    },
    talkgroupId: {
        type: Sequelize.DataTypes.INTEGER,
        allowNull: true,
    },
    type: {
        type: Sequelize.DataTypes.STRING,
        allowNull: true,
    },
    usePolling: {
        type: Sequelize.DataTypes.BOOLEAN,
        defaultValue: false,
    },
};
