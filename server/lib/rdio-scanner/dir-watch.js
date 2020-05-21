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

const chokidar = require('chokidar');
const fs = require('fs');
const mime = require('mime-types');
const path = require('path');

class DirWatch {
    constructor(app = {}) {
        this.app = app;

        this.watchers = [];

        this.app.config.dirWatch.forEach((dirWatch = {}) => {
            if (typeof dirWatch.directory !== 'string' || !dirWatch.directory.length) {
                console.error(`Invalid dirWatch.directory: ${dirWatch.directory}`);

                return;
            }

            if (typeof dirWatch.extension !== 'string' || !dirWatch.extension.length) {
                console.error(`Invalid dirWatch.extension: ${dirWatch.extension}`);

                return;
            }

            if (typeof dirWatch === 'undefined' || !dirWatch.system === null) {
                console.error(`Invalid dirWatch.system: ${dirWatch.system}`);

                return;
            }

            const dir = path.resolve(__dirname, '../../..', dirWatch.directory);

            const watcher = chokidar.watch(dir, {
                awaitWriteFinish: true,
                ignoreInitial: true,
                recursive: true,
                usePolling: typeof dirWatch.usePolling === 'boolean' ? dirWatch.usePolling : false,
            });

            watcher.on('add', (filename) => {
                switch (dirWatch.type) {
                    case 'trunk-recorder':
                        this.importTrunkRecorderType(dirWatch, filename);
                        break;

                    case 'sdrtrunk':
                        this.importSdrTrunkType(dirWatch, filename);
                        break;

                    default:
                        this.importDefaultType(dirWatch, filename);
                }
            });

            this.watchers.push(watcher);
        });
    }

    async importDefaultType(dirWatch, filename) {
        const file = path.parse(filename);

        if (file.ext === `.${dirWatch.extension}`) {
            const audio = fs.readFileSync(filename);

            const audioName = file.base;

            const audioType = mime.lookup(file.base);

            const dateTime = new Date(fs.statSync(filename).ctime);

            const frequency = parseInt(dirWatch.frequency, 10) || null;

            const system = this.parseId(dirWatch.system, filename);

            const talkgroup = this.parseId(dirWatch.talkgroup, filename);

            await this.app.controller.importCall({
                audio,
                audioName,
                audioType,
                dateTime,
                frequency,
                system,
                talkgroup,
            });

            if (dirWatch.deleteAfter) {
                this.unlink(filename);
            }
        }
    }

    async importSdrTrunkType(dirWatch, filename) {
        const file = path.parse(filename);

        if (file.ext === '.mbe') {
            const audioFile = path.resolve(file.dir, `${file.name}.${dirWatch.extension}`);

            if (fs.existsSync(audioFile)) {
                const audio = fs.readFileSync(audioFile);

                const audioName = file.base;

                const audioType = mime.lookup(audioFile);

                const system = this.parseId(dirWatch.system, filename);

                try {
                    const meta = JSON.parse(fs.readFileSync(filename, 'utf8'));

                    await this.app.controller.importSdrtrunk(audio, audioName, audioType, system, meta);

                    if (dirWatch.deleteAfter) {
                        this.unlink(audioFile);

                        this.unlink(filename);
                    }

                } catch (error) {
                    console.error(`Error loading file ${filename}`);

                    return;
                }
            }
        }
    }

    async importTrunkRecorderType(dirWatch, filename) {
        const file = path.parse(filename);

        if (file.ext === '.json') {
            const audioFile = path.resolve(file.dir, `${file.name}.${dirWatch.extension}`);

            if (fs.existsSync(audioFile)) {
                const audio = fs.readFileSync(audioFile);

                const audioName = file.base;

                const audioType = mime.lookup(audioFile);

                const system = this.parseId(dirWatch.system, filename);

                try {
                    const meta = JSON.parse(fs.readFileSync(filename, 'utf8'));

                    await this.app.controller.importTrunkRecorder(audio, audioName, audioType, system, meta);

                    if (dirWatch.deleteAfter) {
                        this.unlink(audioFile);

                        this.unlink(filename);
                    }

                } catch (error) {
                    console.error(`Error loading file ${filename}`);

                    return;
                }
            }
        }
    }

    parseId(id, filename) {
        if (typeof id === 'number') {
            return id;

        } else if (typeof id === 'string') {
            try {
                const regExp = new RegExp(id);

                return parseInt(filename.replace(regExp, '$1'), 10);

            } catch (error) {
                console.log(`Invalid RegExp: ${id}`);

                return NaN;
            }

        } else {
            return NaN;
        }
    }

    unlink(filename) {
        try {
            setTimeout(() => fs.unlinkSync(filename), 1000);

        } catch (error) {
            console.error(`Unable to delete ${filename}`);
        }
    }
}

module.exports = DirWatch;
