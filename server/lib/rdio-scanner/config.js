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

class Config {
    constructor(config = {}) {
        this.access = config.access;

        this.allowDownload = typeof config.allowDownload === 'boolean' ? config.allowDownload : true;

        this.apiKeys = config.apiKeys || ['b29eb8b9-9bcd-4e6e-bb4f-d244ada12736'];

        this.dirWatch = Array.isArray(config.dirWatch) ? config.dirWatch : [];

        this.downstreams = Array.isArray(config.downstreams) ? config.downstreams : [];

        this.pruneDays = typeof config.pruneDays === 'number' ? config.pruneDays : 7;

        this.systems = Array.isArray(config.systems) ? config.systems : [];

        this.useDimmer = typeof config.useDimmer === 'boolean' ? config.useDimmer : false;

        this.useGroup = typeof config.useGroup === 'boolean' ? config.useGroup : true;

        this.useLed = typeof config.useLed === 'boolean' ? config.useLed : true;
    }
}

module.exports = Config;
