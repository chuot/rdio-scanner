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

class RdioScannerTag extends Sequelize.Model { }

export function tagFactory(ctx) {
    RdioScannerTag.init(tagFactory.schema, {
        modelName: 'rdioScannerTag',
        sequelize: ctx.sequelize,
        timestamps: false,
    });

    return RdioScannerTag;
}

tagFactory.schema = {
    _id: {
        type: Sequelize.DataTypes.INTEGER,
        primaryKey: true,
        autoIncrement: true,
    },
    label: {
        type: Sequelize.DataTypes.STRING,
        allowNull: false,
    },
};
