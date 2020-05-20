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
            if (typeof dirWatch.directory === 'string' && dirWatch.directory.length) {
                const dir = path.resolve(__dirname, '../../..', dirWatch.directory);

                const watcher = chokidar.watch(dir, {
                    awaitWriteFinish: true,
                    ignoreInitial: true,
                    recursive: true,
                });

                watcher.on('add', async (filename) => {
                    const file = path.parse(filename);

                    if (
                        (Array.isArray(dirWatch.extension) && dirWatch.extension.some((ext) => file.ext === `.${ext}`)) ||
                        (file.ext === `.${dirWatch.extension}`)
                    ) {
                        const parse = (val) => {
                            if (typeof val === 'number') {
                                return val;

                            } else if (typeof val === 'string') {
                                try {
                                    const regExp = new RegExp(val);

                                    return parseInt(filename.replace(regExp, '$1'), 10);

                                } catch (error) {
                                    console.log(`Invalid RegExp: ${val}`);

                                    return NaN;
                                }

                            } else {
                                return NaN;
                            }
                        }

                        const audio = fs.readFileSync(filename);

                        const audioName = file.base;

                        const audioType = mime.lookup(file.base);

                        const dateTime = new Date(fs.statSync(filename).ctime);

                        const frequency = parseInt(dirWatch.frequency, 10) || null;

                        const system = parse(dirWatch.system);

                        const talkgroup = parse(dirWatch.talkgroup);

                        switch (dirWatch.type) {
                            case 'trunk-recorder': {
                                const metaFile = path.resolve(file.dir, `${file.name}.json`);

                                const metaWatcher = chokidar.watch(metaFile, { awaitWriteFinish: true });

                                const timeout = setTimeout(() => {
                                    if (!metaWatcher.closed) {
                                        metaWatcher.close();
                                    }
                                }, 60 * 60 * 1000);

                                metaWatcher.on('add', async () => {
                                    clearTimeout(timeout);

                                    await metaWatcher.close();

                                    try {
                                        const meta = JSON.parse(fs.readFileSync(metaFile, 'utf8'));

                                        await this.app.controller.importTrunkRecorder(audio, audioName, audioType, system, meta);

                                    } catch (error) {
                                        console.error(`Error loading file ${metaFile}`);

                                        return;
                                    }

                                    if (dirWatch.deleteAfter) {
                                        this.unlink(metaFile);
                                    }
                                });

                                break;
                            }

                            case 'sdrtrunk': {
                                const metaFile = path.resolve(file.dir, `${file.name}.mbe`);

                                const metaWatcher = chokidar.watch(metaFile, { awaitWriteFinish: true });

                                const timeout = setTimeout(() => {
                                    if (!metaWatcher.closed) {
                                        metaWatcher.close();
                                    }
                                }, 60 * 60 * 1000);

                                metaWatcher.on('add', async () => {
                                    clearTimeout(timeout);

                                    await metaWatcher.close();

                                    try {
                                        const meta = JSON.parse(fs.readFileSync(metaFile, 'utf8'));

                                        await this.app.controller.importSdrtrunk(audio, audioName, audioType, system, meta);

                                    } catch (error) {
                                        console.error(`Error loading file ${metaFile}`);

                                        return;
                                    }

                                    if (dirWatch.deleteAfter) {
                                        this.unlink(metaFile);
                                    }
                                });

                                break;
                            }

                            default: {
                                await this.app.controller.importCall({
                                    audio,
                                    audioName,
                                    audioType,
                                    dateTime,
                                    frequency,
                                    system,
                                    talkgroup,
                                });
                            }
                        }

                        if (dirWatch.deleteAfter) {
                            this.unlink(filename);
                        }
                    }
                });

                this.watchers.push(watcher);
            }
        });
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
