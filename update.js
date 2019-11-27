'use strict';

const childProcess = require('child_process');
const path = require('path');

const clientPath = path.resolve(__dirname, 'client');
const serverPath = path.resolve(__dirname, 'server');

process.stdout.write('Pulling new version from github...');
childProcess.execSync('git pull', { silent: true });
process.stdout.write(' done\n');

process.stdout.write('Updating node modules...');
childProcess.execSync('npm install', { cwd: clientPath, silent: true });
childProcess.execSync('npm prune', { cwd: clientPath, silent: true });
childProcess.execSync('npm install', { cwd: serverPath, silent: true });
childProcess.execSync('npm prune', { cwd: serverPath, silent: true });
process.stdout.write(' done\n');

process.stdout.write('Migrating database...');
childProcess.execSync('npm run migrate', { cwd: serverPath, silent: true });
process.stdout.write(' done\n');

process.stdout.write('Building client app...');
childProcess.execSync('npm run build', { cwd: clientPath, silent: true });
process.stdout.write(' done\n');

process.stdout.write('Please restart Rdio Scanner\n')