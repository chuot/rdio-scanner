'use strict';

const childProcess = require('child_process');
const fs = require('fs');
const path = require('path');

const clientPath = path.resolve(__dirname, 'client');
const serverPath = path.resolve(__dirname, 'server');

const missingModules = {
    client: !fs.existsSync(path.resolve(clientPath, 'node_modules')),
    server: !fs.existsSync(path.resolve(serverPath, 'node_modules')),
}

if (missingModules.client || missingModules.server) {
    process.stdout.write('Installing node modules...');

    if (missingModules.client) {
        childProcess.execSync('npm install', { cwd: clientPath, stdio: 'ignore' });
    }

    if (missingModules.server) {
        childProcess.execSync('npm install', { cwd: serverPath, stdio: 'ignore' });
    }

    process.stdout.write(' done\n');
}

const envFile = path.resolve(serverPath, '.env');

if (!fs.existsSync(envFile)) {
    const apiKey = require(`${serverPath}/node_modules/uuid/v4`)();

    const data = [
        `DB_DIALECT=sqlite`,
        `DB_STORAGE=database.sqlite`,
        ``,
        `NODE_ENV=production`,
        `NODE_HOST=0.0.0.0`,
        `NODE_PORT=3000`,
        ``,
        `RDIO_APIKEYS=["${apiKey}"]`,
        `RDIO_PRUNEDAYS=7`,
        ``,
    ].join('\n');

    fs.writeFileSync(envFile, data);

    process.stdout.write(`Default configuration created at ${envFile}\n`);
    process.stdout.write(`Make sure your upload scripts use this API key: ${apiKey}\n`);
}

if (!fs.existsSync(path.resolve(clientPath, 'dist/main.html'))) {
    process.stdout.write('Building client app...');
    childProcess.execSync('npm run build', { cwd: clientPath, stdio: 'ignore' });
    process.stdout.write(' done\n');
}

require(path.resolve(serverPath, 'node_modules/dotenv')).config({ path: envFile });

if (process.env.DB_DIALECT === 'sqlite') {
    const dbFile = path.resolve(serverPath, process.env.DB_STORAGE || 'database.sqlite');

    if (!fs.existsSync(dbFile)) {
        process.stdout.write(`Creating SQLITE database at ${dbFile}...`);
        childProcess.execSync('npm run migrate', { cwd: serverPath, stdio: 'ignore' });
        process.stdout.write(' done\n');
    }
}

childProcess.execSync('node index.js', { cwd: serverPath, stdio: 'inherit' });
