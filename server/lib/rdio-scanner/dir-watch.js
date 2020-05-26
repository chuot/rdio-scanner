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

            switch (dirWatch.type) {
                case 'trunk-recorder':
                    watcher.on('add', (filename) => this.importTrunkRecorderType(dirWatch, filename));
                    break;

                case 'sdrtrunk':
                    watcher.on('add', (filename) => this.importSdrTrunkType(dirWatch, filename));
                    break;

                default:
                    if (typeof dirWatch.delay === 'number' && dirWatch.delay >= 100) {
                        const debounces = {};

                        const debounceFn = (filename) => setTimeout(() => {
                            if (this.exists(filename)) {
                                if (this.statFile(filename).size > 44) {
                                    delete debounces[filename];

                                    this.importDefaultType(dirWatch, filename);

                                } else {
                                    debounces[filename] = debounceFn(filename);
                                }
                            }
                        }, dirWatch.delay);

                        watcher.on('raw', (_, filename) => {
                            filename = path.resolve(dir, filename);

                            if (debounces[filename]) {
                                clearTimeout(debounces[filename]);
                            }

                            debounces[filename] = debounceFn(filename);
                        });

                    } else {
                        watcher.on('add', (filename) => this.importDefaultType(dirWatch, filename));
                    }
            }
        });
    }

    async importDefaultType(dirWatch, filename) {
        const file = path.parse(filename);

        if (file.ext === `.${dirWatch.extension}`) {
            const audio = this.readFile(filename);

            const audioName = file.base;

            const audioType = mime.lookup(file.base);

            const dateTime = new Date(this.statFile(filename).ctime);

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
                const audio = this.readFile(audioFile);

                const audioName = file.base;

                const audioType = mime.lookup(audioFile);

                const system = this.parseId(dirWatch.system, filename);

                try {
                    const meta = JSON.parse(this.readFile(filename, 'utf8'));

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
                const audio = this.readFile(audioFile);

                const audioName = file.base;

                const audioType = mime.lookup(audioFile);

                const system = this.parseId(dirWatch.system, filename);

                try {
                    const meta = JSON.parse(this.readFile(filename, 'utf8'));

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

    exists(filename) {
        try {
            return fs.existsSync(filename);

        } catch (error) {
            return false;
        }
    }

    readFile(filename, mode) {
        try {
            return fs.readFileSync(filename, mode);

        } catch (error) {
            console.error(`Unable to read ${filename}`);
        }
    }

    statFile(filename) {
        try {
            return fs.statSync(filename);

        } catch (error) {
            console.error(`Unable to stat ${filename}`);

            return {};
        }
    }

    unlink(filename) {
        try {
            fs.unlinkSync(filename);

        } catch (error) {
            console.error(`Unable to delete ${filename}`);
        }
    }
}

module.exports = DirWatch;
