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

const { execSync } = require('child_process');
const { existsSync } = require('fs');
const path = require('path');

try {
    const stdio = `${!!process.env.DEBUG}` === 'true' ? 'inherit' : 'pipe';

    const clientPath = path.resolve(__dirname, 'client');
    const serverPath = path.resolve(__dirname, 'server');

    const missingModules = {
        client: !existsSync(path.resolve(clientPath, 'node_modules')),
        server: !existsSync(path.resolve(serverPath, 'node_modules')),
    }

    if (missingModules.client || missingModules.server) {
        process.stdout.write('Installing node modules...');

        if (missingModules.client) {
            execSync('npm ci', { cwd: clientPath, stdio });
        }

        if (missingModules.server) {
            execSync('npm ci', { cwd: serverPath, stdio });
        }

        process.stdout.write(' done\n');
    }

    if (!existsSync(path.resolve(clientPath, 'dist/rdio-scanner/index.html'))) {
        process.stdout.write('Building client app...');
        execSync('npm run build', { cwd: clientPath, stdio });
        process.stdout.write(' done\n');
    }

    const args = process.argv.slice(2).join(' ');

    execSync(`node . ${args}`, { cwd: serverPath, stdio: 'inherit' });

} catch (error) {
    process.stderr.write('\n\nAn error has occured. Please re-run the command like this: \'DEBUG=true node run.js\'\n');
}