/*
 * *****************************************************************************
 * Copyright (C) 2019-2021 Chrystian Huot
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

const { spawn, spawnSync } = require('child_process');
const chokidar = require('chokidar');
const fs = require('fs');
const mime = require('mime-types');
const path = require('path');

const RECORDER_TYPE = {
    default: 'default',
    sdrTrunk: 'sdr-trunk',
    trunkRecorder: 'trunk-recorder',
};

class DirWatch {
    constructor(ctx = {}) {
        if (!Array.isArray(ctx.config.dirWatch)) {
            ctx.config.dirWatch = [];
        }

        this.config = ctx.config.dirWatch;

        this.controller = ctx.controller;

        this.ffprobe = !spawnSync('ffprobe', ['-version']).error;

        this.registredDirectories = [];

        this.config.forEach((dirWatch = {}) => {
            if (typeof dirWatch.directory !== 'string' || !dirWatch.directory.length) {
                console.error(`Config: No dirWatch.directory defined`);

                return;
            }

            if (dirWatch.type === RECORDER_TYPE.default && (typeof dirWatch.extension !== 'string' || !dirWatch.extension.length)) {
                console.error(`Config: Mo dirWatch.extension defined for dirwatch.type='${RECORDER_TYPE.default}'`);

                return;
            }

            if (typeof dirWatch.delay !== 'number' || dirWatch.delay < 100) {
                delete dirWatch.delay;
            }

            if (typeof dirWatch.deleteAfter !== 'boolean') {
                dirWatch.deleteAfter = false;
            }

            if (typeof dirWatch.disabled === 'boolean' && !dirWatch.disabled) {
                delete dirWatch.disabled;
            }

            if (dirWatch.frequency === null || typeof dirWatch.frequency !== 'number') {
                delete dirWatch.frequency;
            }

            if (dirWatch.mask === null || (
                !(typeof dirWatch.mask === 'string' && dirWatch.mask.length) &&
                !(Array.isArray(dirWatch.mask) && dirWatch.mask.every((mask) => typeof mask === 'string' && mask.length))
            )) {
                delete dirWatch.mask;
            }

            dirWatch.usePolling = dirWatch.usePolling === true ? true
                : typeof dirWatch.usePolling === 'number' && dirWatch.usePolling >= 1000 ? dirWatch.usePolling
                    : false;

            if (!dirWatch.usePolling) {
                delete dirWatch.usePolling;
            }

            if (dirWatch.system === null && typeof dirWatch.system !== 'number' && dirWatch.system !== 'string') {
                delete dirWatch.system;
            }

            if (dirWatch.talkgroup === null && typeof dirWatch.talkgroup !== 'number' && dirWatch.talkgroup !== 'string') {
                delete dirWatch.talkgroup;
            }

            if ([RECORDER_TYPE.sdrTrunk, RECORDER_TYPE.trunkRecorder].includes(dirWatch.type)) {
                if (typeof dirWatch.extension !== 'string' && typeof dirWatch.extension !== 'undefined') {
                    delete dirWatch.extension;
                }

                if (dirWatch.type === RECORDER_TYPE.sdrTrunk && typeof dirWatch.system !== 'undefined') {
                    delete dirWatch.system;
                }

            } else {
                delete dirWatch.type;
            }

            const dir = path.resolve(__dirname, '../../..', dirWatch.directory);

            if (this.registredDirectories.includes(dir)) {
                console.error(`Config: Duplicate directory definitions ${dir}`);

                return;

            } else if (dirWatch.type === RECORDER_TYPE.sdrTrunk && !this.ffprobe) {
                console.error(`Config: ffprobe is missing, dirWatch.type='${RECORDER_TYPE.sdrTrunk}' are ignored`);

                return;

            } else {
                this.registredDirectories.push(dir);
            }
        });

        ctx.once('ready', () => {
            this.config.filter((dirWatch) => !dirWatch.disabled).forEach((dirWatch = {}) => {
                const options = {
                    awaitWriteFinish: true,
                    ignoreInitial: !dirWatch.deleteAfter,
                    recursive: true,
                    usePolling: !!dirWatch.usePolling || false,
                };

                if (options.usePolling) {
                    options.binaryInterval = options.interval = dirWatch.usePolling === true ? 1000 : dirWatch.usePolling;
                }

                const watcher = chokidar.watch(path.resolve(__dirname, '../../..', dirWatch.directory), options);

                switch (dirWatch.type) {
                    case RECORDER_TYPE.sdrTrunk:
                        watcher.on('add', (filename) => this.importSdrTrunkType(dirWatch, filename));
                        break;

                    case RECORDER_TYPE.trunkRecorder:
                        watcher.on('add', (filename) => this.importTrunkRecorderType(dirWatch, filename));
                        break;

                    default:
                        if (dirWatch.delay >= 100) {
                            const debounces = {};

                            const debounceFn = (filename) => setTimeout(() => {
                                delete debounces[filename];

                                if (this.exists(filename)) {
                                    const stat = this.statFile(filename);

                                    if (stat.isFile()) {
                                        if (stat.size > 44) {
                                            this.importDefaultType(dirWatch, filename);

                                        } else {
                                            debounces[filename] = debounceFn(filename);
                                        }
                                    }
                                }
                            }, dirWatch.delay);

                            watcher.on('raw', (_, filename, details) => {
                                filename = details.watchedPath.indexOf(filename) === -1 
                                    ? path.join(details.watchedPath, filename)
                                    : details.watchedPath;

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
        });
    }

    async importDefaultType(dirWatch, filename) {
        const file = path.parse(filename);

        if (file.ext === `.${dirWatch.extension}`) {
            const call = {};

            if (dirWatch.mask) {
                const parsedMask = Array.isArray(dirWatch.mask)
                    ? dirWatch.mask.reduce((meta, mask) => meta !== null ? meta : this.parseMask(mask, filename), null)
                    : this.parseMask(dirWatch.mask, filename);

                if (parsedMask) {
                    Object.assign(call, parsedMask);

                } else {
                    console.error(`DirWatch: No matching mask for file ${filename}`);

                    return;
                }
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

            if (call.system === undefined) {
                call.system = this.parseRegex(dirWatch.system, filename);
            }

            if (call.talkgroup === undefined) {
                call.talkgroup = this.parseRegex(dirWatch.talkgroup, filename);
            }

            await this.controller.importCall(call);

            if (dirWatch.deleteAfter) {
                this.unlink(filename);
            }
        }
    }

    async importSdrTrunkType(dirWatch, filename) {
        const file = path.parse(filename);

        const extension = dirWatch.extension || 'mp3';

        if (file.ext === `.${extension}`) {
            const audioFile = path.resolve(file.dir, `${file.name}.${extension}`);

            if (this.exists(audioFile)) {
                const audio = this.readFile(audioFile);

                const audioName = file.base;

                const audioType = mime.lookup(audioFile);

                const proc = spawn('ffprobe', [
                    '-loglevel',
                    'quiet',
                    '-print_format',
                    'json',
                    '-show_entries',
                    'format',
                    '-',
                ]);

                let probe = Buffer.from([]);

                proc.on('close', async () => {
                    try {
                        probe = JSON.parse(probe.toString());

                    } catch (error) {
                        console.error(`DirWatch: Unable to process ffprobe output for file ${filename}`);
                    }

                    if (!(probe && probe.format && probe.format.tags)) {
                        console.error(`DirWatch: Unknown ffprobe json output for file ${filename}`);

                        return;
                    }

                    const tags = probe.format.tags;

                    if (typeof tags.TDAT !== 'string' || !tags.TDAT.length) {
                        console.error(`DirWatch: Unable to fetch date from MP3 tags for file ${filename}`);

                        return;
                    }

                    const dateTime = new Date(tags.TDAT);

                    const frequency = parseInt(tags.comment.match(/Frequency:([0-9]+)/)[1], 10);

                    if (isNaN(frequency)) {
                        console.error(`DirWatch: Unable to fetch frequency from MP3 tags for file ${filename}`);

                        return;
                    }

                    const sys = this.controller.config.systems.find((system) => system.label === tags.TIT1);

                    if (!sys) {
                        console.error(`DirWatch: Unable to find system label ${tags.TIT1} for file ${filename}`);

                        return;
                    }

                    const system = sys.id;

                    const source = parseInt(tags.artist, 10) || null;

                    const talkgroup = typeof tags.title === 'string' && parseInt((tags.title.match(/([0-9]+)/) || [])[1], 10);

                    if (!talkgroup) {
                        console.error(`DirWatch: Unable to fetch talkgroup id from MP3 tags for file ${filename}`);

                        return;
                    }

                    const call = {
                        audio,
                        audioName,
                        audioType,
                        dateTime,
                        frequency,
                        source,
                        system,
                        talkgroup,
                    };

                    try {
                        await this.controller.importCall(call);

                        if (dirWatch.deleteAfter) {
                            this.unlink(filename);
                        }

                    } catch (error) {
                        console.error(`DirWatch: Error importing file ${audioFile}, ${error.message}`);

                        return;
                    }
                });

                proc.on('error', (error) => {
                    console.error(`DirWatch: Error while fetching MP3 tags for file ${filename}, ${error.message}`);
                });

                proc.stdin.on('error', (error) => {
                    console.error(`DirWatch: Error while fetching MP3 tags for file ${filename}, ${error.message}`);
                });

                proc.stdout.on('data', (data) => probe = Buffer.concat([probe, data]));

                process.nextTick(() => {
                    proc.stdin.setEncoding('binary');
                    proc.stdin.write(audio);
                    proc.stdin.end();
                });
            }
        }
    }

    async importTrunkRecorderType(dirWatch, filename) {
        const file = path.parse(filename);

        if (file.ext === '.json') {
            const audioFile = path.resolve(file.dir, `${file.name}.${dirWatch.extension || 'wav'}`);

            if (this.exists(audioFile)) {
                const audio = this.readFile(audioFile);

                const audioName = file.base;

                const audioType = mime.lookup(audioFile);

                const system = this.parseRegex(dirWatch.system, filename);

                let meta;

                try {
                    meta = JSON.parse(this.readFile(filename, 'utf8'));

                } catch (error) {
                    console.error(`DirWatch: Error parsing json file ${filename}, ${error.message}`);

                    return;
                }

                try {
                    await this.controller.importTrunkRecorder(audio, audioName, audioType, system, meta);

                    if (dirWatch.deleteAfter) {
                        this.unlink(audioFile);

                        this.unlink(filename);
                    }

                } catch (error) {
                    console.error(`DirWatch: Error importing file ${audioFile}, ${error.message}`);

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
            hz: { tag: '#HZ', regex: '[\\d]+' },
            khz: { tag: '#KHZ', regex: '[\\d\\.]+' },
            mhz: { tag: '#MHZ', regex: '[\\d\\.]+' },
            time: { tag: '#TIME', regex: '[\\d:]+' },
            system: { tag: '#SYS', regex: '\\d+' },
            talkgroup: { tag: '#TG', regex: '\\d+' },
            unit: { tag: '#UNIT', regex: '\\d+' },
            ztime: { tag: '#ZTIME', regex: '[\\d:]+' },
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

            } else if (data.date && data.ztime) {
                const date = data.date.replace(/[^\d]]+/g, '').replace(/(\d{4})(\d{2})(\d{2})/g, '$1-$2-$3');
                const time = data.ztime.replace(/[^\\d]]+/g, '').replace(/(\d)(?=(\d{2})+$)/g, '$1:');

                const dateTime = new Date(`${date}T${time}Z`);

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

            if (data.hz || data.khz || data.mhz) {
                const freq = +(data.hz || data.khz || data.mhz);

                if (freq) {
                    call.frequency = data.hz ? freq : data.khz ? freq * 1000 : data.mhz * 1000000;
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

            return call;

        } else {
            return null;
        }
    }

    parseRegex(regex, filename) {
        if (typeof regex === 'number') {
            return regex;

        } else if (typeof regex === 'string') {
            try {
                const regExp = new RegExp(regex);

                return parseInt(filename.replace(regExp, '$1'), 10);

            } catch (error) {
                console.log(`DirWatch: Invalid RegExp: ${regex}, ${error.message}`);

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
            console.error(`DirWatch: Unable to read ${filename}, ${error.message}`);
        }
    }

    statFile(filename) {
        try {
            return fs.statSync(filename);

        } catch (error) {
            console.error(`DirWatch: Unable to stat ${filename}, ${error.message}`);

            return {};
        }
    }

    unlink(filename) {
        try {
            fs.unlinkSync(filename);

        } catch (error) {
            console.error(`DirWatch: Unable to delete ${filename}, ${error.message}`);
        }
    }
}

module.exports = DirWatch;
