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

import { accessFactory } from './models/access.js';
import { apiKeyFactory } from './models/api-key.js';
import { callFactory } from './models/call.js';
import { configFactory } from './models/config.js';
import { dirWatchFactory } from './models/dir-watch.js';
import { downstreamFactory } from './models/downstream.js';
import { groupFactory } from './models/group.js';
import { logFactory } from './models/log.js';
import { systemFactory } from './models/system.js';
import { tagFactory } from './models/tag.js';

export class Models {
    constructor(ctx) {
        this.access = accessFactory(ctx);

        this.apiKey = apiKeyFactory(ctx);

        this.call = callFactory(ctx);

        this.config = configFactory(ctx);

        this.dirWatch = dirWatchFactory(ctx);

        this.downstream = downstreamFactory(ctx);

        this.group = groupFactory(ctx);

        this.log = logFactory(ctx);

        this.system = systemFactory(ctx);

        this.tag = tagFactory(ctx);
    }
}
