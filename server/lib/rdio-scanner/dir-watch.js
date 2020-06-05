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
            const call = {};

            if (dirWatch.mask) {
                Object.assign(call, this.parseMask(dirWatch.mask, filename));
            }

            call.audio = this.readFile(filename);

            call.audioName = file.base;

            call.audioType = mime.lookup(file.base);

            if (!call.dateTime) {
                call.dateTime = new Date(this.statFile(filename).ctime || '');
            }

            if (dirWatch.frequency) {
                call.frequency = parseInt(dirWatch.frequency, 10) || null;
            }

            if (typeof call.system === 'undefined') {
                call.system = this.parseRegex(dirWatch.system, filename);
            }

            if (typeof call.talkgroup === 'undefined') {
                call.talkgroup = this.parseRegex(dirWatch.talkgroup, filename);
            }

            await this.app.controller.importCall(call);

            if (dirWatch.deleteAfter) {
                this.unlink(filename);
            }
        }
    }

    async importTrunkRecorderType(dirWatch, filename) {
        const file = path.parse(filename);

        if (file.ext === '.json') {
            const audioFile = path.resolve(file.dir, `${file.name}.${dirWatch.extension}`);

            if (this.exists(audioFile)) {
                const audio = this.readFile(audioFile);

                const audioName = file.base;

                const audioType = mime.lookup(audioFile);

                const system = this.parseRegex(dirWatch.system, filename);

                let meta;

                try {
                    meta = JSON.parse(this.readFile(filename, 'utf8'));

                } catch (error) {
                    console.error(`DirWatch: Error parsing json file ${filename}`);

                    return;
                }

                try {
                    await this.app.controller.importTrunkRecorder(audio, audioName, audioType, system, meta);

                    if (dirWatch.deleteAfter) {
                        this.unlink(audioFile);

                        this.unlink(filename);
                    }

                } catch (error) {
                    console.error(`DirWatch: Error importing file ${audioFile}`, error);

                    return;
                }

            } else {
                console.error(`DirWatch: Error missing file ${audioFile}`);

                return;
            }
        }
    }

    parseMask(mask, filename) {
        const call = {};

        if (typeof filename !== 'string' || !filename.length) {
            return call;
        }

        if (typeof mask !== 'string' || !mask.length) {
            return call;
        }

        const meta = {
            date: { tag: '#DATE', regex: '[\\d-]+' },
            hz: { tag: '#HZ', regex: '[\\d-]+' },
            time: { tag: '#TIME', regex: '[\\d:]+' },
            system: { tag: '#SYS', regex: '\\d+' },
            talkgroup: { tag: '#TG', regex: '\\d+' },
            unit: { tag: '#UNIT', regex: '\\d+' },
        };

        let data = [];

        let regex = mask;

        Object.keys(meta).forEach((key) => {
            const index = mask.indexOf(meta[key].tag);

            if (index !== -1) {
                data.push([key, index]);

                regex = regex.replace(meta[key].tag, `(${meta[key].regex})`);
            }
        });

        const match = filename.match(regex);

        if (Array.isArray(match)) {
            data = data.sort((a, b) => a[1] - b[1]).reduce((obj, val, index) => {
                obj[val[0]] = match[index + 1];

                return obj;
            }, {});

            if (data.date && data.time) {
                const date = data.date.replace(/[^\d]]+/g, '').replace(/(\d{4})(\d{2})(\d{2})/g, '$1-$2-$3');
                const time = data.time.replace(/[^\\d]]+/g, '').replace(/(\d)(?=(\d{2})+$)/g, '$1:');

                const dateTime = new Date(`${date}T${time}`);

                if (!isNaN(dateTime.getTime())) {
                    call.dateTime = dateTime;
                }

            } else if (data.date) {
                const val = parseInt(data.date, 10);

                if (val > 999999) {
                    call.dateTime = new Date(1970, 0, 1);

                    call.dateTime.setSeconds(val);
                }
            }

            if (data.hz) {
                const freq = parseInt(data.hz, 10);

                if (freq) {
                    call.frequency = freq;
                }
            }

            if (data.system) {
                call.system = parseInt(data.system, 10);
            }

            if (data.talkgroup) {
                call.talkgroup = parseInt(data.talkgroup, 10);
            }

            if (data.unit) {
                call.sources = [{ pos: 0, src: parseInt(data.unit, 10) }];
            }
        }

        return call;
    }

    parseRegex(regex, filename) {
        if (typeof regex === 'number') {
            return regex;

        } else if (typeof regex === 'string') {
            try {
                const regExp = new RegExp(regex);

                return parseInt(filename.replace(regExp, '$1'), 10);

            } catch (error) {
                console.log(`DirWatch: Invalid RegExp: ${regex}`);

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
            console.error(`DirWatch: Unable to read ${filename}`);
        }
    }

    statFile(filename) {
        try {
            return fs.statSync(filename);

        } catch (error) {
            console.error(`DirWatch: Unable to stat ${filename}`);

            return {};
        }
    }

    unlink(filename) {
        try {
            fs.unlinkSync(filename);

        } catch (error) {
            console.error(`DirWatch: Unable to delete ${filename}`);
        }
    }
}

module.exports = DirWatch;
