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

const CallUpload = require('./call-upload');
const TrunkRecorderCallUpload = require('./trunk-recorder-call-upload');

class Routes {
    constructor(app) {
        this.callUpload = new CallUpload(app);
        this.trunkRecorderCallUpload = new TrunkRecorderCallUpload(app);

        setTimeout(() => {
            app.router.use(this.callUpload.path, this.callUpload.middleware);
            app.router.use(this.trunkRecorderCallUpload.path, this.trunkRecorderCallUpload.middleware);
        });
    }
}

module.exports = Routes;
