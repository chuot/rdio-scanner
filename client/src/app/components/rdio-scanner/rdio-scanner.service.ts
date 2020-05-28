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

import { DOCUMENT } from '@angular/common';
import { EventEmitter, Inject, Injectable, OnDestroy } from '@angular/core';
import {
    RdioScannerAvoidOptions,
    RdioScannerCall,
    RdioScannerConfig,
    RdioScannerEvent,
    RdioScannerGroup,
    RdioScannerGroupStatus,
    RdioScannerLiveFeedMap,
    RdioScannerSearchOptions,
} from './rdio-scanner';

declare var webkitAudioContext: any;

enum WsCallFlag {
    Download = 'd',
    Play = 'p',
}

enum WsCommand {
    Call = 'CAL',
    Config = 'CFG',
    ListCall = 'LCL',
    LiveFeedMap = 'LFM',
    Pin = 'PIN',
}

@Injectable()
export class AppRdioScannerService implements OnDestroy {
    static LOCAL_STORAGE_KEY = 'rdio-scanner';

    event = new EventEmitter<RdioScannerEvent>();

    private audioContext: AudioContext | null = null;
    private audioSource: AudioBufferSourceNode | null = null;
    private audioStartTime = NaN;
    private audioTimer: any = null;

    private call: RdioScannerCall;
    private callPrevious: RdioScannerCall;
    private callQueue: RdioScannerCall[] = [];

    private config: RdioScannerConfig;

    private groups: RdioScannerGroup[];

    private liveFeedActive = false;
    private liveFeedMap: RdioScannerLiveFeedMap;
    private liveFeedMapPriorToHoldSystem: RdioScannerLiveFeedMap | null = null;
    private liveFeedMapPriorToHoldTalkgroup: RdioScannerLiveFeedMap | null = null;
    private liveFeedPaused = false;

    private skipDelay: any;

    private webSocket: WebSocket;

    constructor(@Inject(DOCUMENT) private document: Document) {
        this.bootstrapAudio();

        this.liveFeedRestore();

        this.wsOpen();
    }

    authenticate(password: string): void {
        this.wsSend(WsCommand.Pin, btoa(password));
    }

    avoid(options: RdioScannerAvoidOptions = {}): void {
        if (this.liveFeedMapPriorToHoldSystem) {
            this.liveFeedMapPriorToHoldSystem = null;
        }

        if (this.liveFeedMapPriorToHoldTalkgroup) {
            this.liveFeedMapPriorToHoldTalkgroup = null;
        }

        if (typeof options.all === 'boolean') {
            Object.keys(this.liveFeedMap).map((sys: string) => +sys).forEach((sys: number) => {
                Object.keys(this.liveFeedMap[sys]).map((tg: string) => +tg).forEach((tg: number) => {
                    this.liveFeedMap[sys][tg] = typeof options.status === 'boolean' ? options.status : options.all;
                });
            });

        } else if (options.call) {
            const sys = options.call.system;
            const tg = options.call.talkgroup;

            this.liveFeedMap[sys][tg] = typeof options.status === 'boolean' ? options.status : !this.liveFeedMap[sys][tg];

        } else if (options.system && options.talkgroup) {
            const sys = options.system.id;
            const tg = options.talkgroup.id;

            this.liveFeedMap[sys][tg] = typeof options.status === 'boolean' ? options.status : !this.liveFeedMap[sys][tg];

        } else if (options.system && !options.talkgroup) {
            const sys = options.system.id;

            Object.keys(this.liveFeedMap[sys]).map((tg: string) => +tg).forEach((tg: number) => {
                this.liveFeedMap[sys][tg] = typeof options.status === 'boolean' ? options.status : !this.liveFeedMap[sys][tg];
            });

        } else {
            const call = this.call || this.callPrevious;

            if (call) {
                const sys = call.system;
                const tg = call.talkgroup;

                this.liveFeedMap[sys][tg] = typeof options.status === 'boolean' ? options.status : !this.liveFeedMap[sys][tg];
            }
        }

        this.cleanQueue();

        if (this.call && !(this.liveFeedMap && this.liveFeedMap[this.call.system] &&
            this.liveFeedMap[this.call.system][this.call.talkgroup])) {

            this.skip();
        }

        this.buildGroups();

        this.liveFeedStore();

        if (this.liveFeedActive) {
            this.liveFeedStart();
        }

        this.event.emit({
            groups: this.groups,
            holdSys: false,
            holdTg: false,
            map: this.liveFeedMap,
            queue: this.callQueue.length,
        });
    }

    holdSystem(resubscribe = true): void {
        const call = this.call || this.callPrevious;

        if (call && this.liveFeedMap) {
            if (this.liveFeedMapPriorToHoldSystem) {
                this.liveFeedMap = this.liveFeedMapPriorToHoldSystem;
                this.liveFeedMapPriorToHoldSystem = null;

            } else {
                if (this.liveFeedMapPriorToHoldTalkgroup) {
                    this.holdTalkgroup(false);
                }

                this.liveFeedMapPriorToHoldSystem = this.liveFeedMap;

                this.liveFeedMap = Object.keys(this.liveFeedMap).map((sys) => +sys).reduce((sysMap, sys) => {
                    const allOn = Object.keys(this.liveFeedMap[sys]).every((tg) => !this.liveFeedMap[sys][tg]);

                    sysMap[sys] = Object.keys(this.liveFeedMap[sys]).map((tg) => +tg).reduce((tgMap, tg) => {
                        if (sys === call.system) {
                            tgMap[tg] = allOn || this.liveFeedMap[sys][tg];
                        } else {
                            tgMap[tg] = false;
                        }

                        return tgMap;
                    }, {});

                    return sysMap;
                }, {});

                this.cleanQueue();
            }

            this.buildGroups();

            if (resubscribe) {
                if (this.liveFeedActive) {
                    this.liveFeedStart();
                }
            }

            this.event.emit({
                groups: this.groups,
                holdSys: !!this.liveFeedMapPriorToHoldSystem,
                holdTg: false,
                map: this.liveFeedMap,
                queue: this.callQueue.length,
            });
        }
    }

    holdTalkgroup(resubscribe = true): void {
        const call = this.call || this.callPrevious;

        if (call && this.liveFeedMap) {
            if (this.liveFeedMapPriorToHoldTalkgroup) {
                this.liveFeedMap = this.liveFeedMapPriorToHoldTalkgroup;
                this.liveFeedMapPriorToHoldTalkgroup = null;

            } else {
                if (this.liveFeedMapPriorToHoldSystem) {
                    this.holdSystem(false);
                }

                this.liveFeedMapPriorToHoldTalkgroup = this.liveFeedMap;

                this.liveFeedMap = Object.keys(this.liveFeedMap).map((sys) => +sys).reduce((sysMap, sys) => {
                    sysMap[sys] = Object.keys(this.liveFeedMap[sys]).map((tg) => +tg).reduce((tgMap, tg) => {
                        if (sys === call.system) {
                            tgMap[tg] = tg === call.talkgroup;

                        } else {
                            tgMap[tg] = false;
                        }

                        return tgMap;
                    }, {});

                    return sysMap;
                }, {});

                this.cleanQueue();
            }

            this.buildGroups();

            if (resubscribe) {
                if (this.liveFeedActive) {
                    this.liveFeedStart();
                }
            }

            this.event.emit({
                groups: this.groups,
                holdSys: false,
                holdTg: !!this.liveFeedMapPriorToHoldTalkgroup,
                map: this.liveFeedMap,
                queue: this.callQueue.length,
            });
        }
    }

    liveFeed(status: boolean = !this.liveFeedActive): void {
        if (status) {
            this.liveFeedStart();

        } else {
            this.liveFeedStop();
        }
    }

    loadAndDownload(id: string): void {
        if (this.config.allowDownload) {
            this.getCall(id, WsCallFlag.Download);
        }
    }

    loadAndPlay(id: string): void {
        this.getCall(id, WsCallFlag.Play);
    }

    ngOnDestroy(): void {
        this.wsClose();
    }

    pause(status = !this.liveFeedPaused): void {
        if (this.audioContext) {
            this.liveFeedPaused = status;

            if (this.liveFeedPaused) {
                this.audioContext.suspend();

            } else {
                this.audioContext.resume();

                this.play();
            }

            this.event.emit({ pause: this.liveFeedPaused });
        }
    }

    play(call?: RdioScannerCall): void {
        if (this.audioContext && !this.liveFeedPaused) {
            if (!call && !this.call && this.callQueue.length) {
                call = this.callQueue.shift();

                this.event.emit({ queue: this.callQueue.length });
            }

            if (call && call.audio) {
                this.stop(false);

                this.call = call;

                const arrayBuffer = new ArrayBuffer(call.audio.data.length);
                const arrayBufferView = new Uint8Array(arrayBuffer);

                for (let i = 0; i < call.audio.data.length; i++) {
                    arrayBufferView[i] = call.audio.data[i];
                }

                this.audioContext.decodeAudioData(arrayBuffer, (buffer) => {
                    this.event.emit({ call });

                    this.audioContext.resume().then(() => {
                        this.audioSource = this.audioContext.createBufferSource();

                        this.audioSource.buffer = buffer;
                        this.audioSource.connect(this.audioContext.destination);
                        this.audioSource.onended = () => this.skip();
                        this.audioSource.start();

                        this.audioTimer = setInterval(() => {
                            if (!this.liveFeedPaused && !isNaN(this.audioContext.currentTime)) {
                                if (isNaN(this.audioStartTime)) {
                                    this.audioStartTime = this.audioContext.currentTime;
                                }

                                this.event.emit({ time: this.audioContext.currentTime - this.audioStartTime });
                            }
                        }, 500);
                    });
                }, () => this.skip());
            }
        }
    }

    queue(call: RdioScannerCall): void {
        const sys = call.system;
        const tg = call.talkgroup;

        if (call?.audio && this.liveFeedMap && this.liveFeedMap[sys] && this.liveFeedMap[sys][tg]) {
            this.callQueue.push(call);

            this.event.emit({ queue: this.callQueue.length });

            this.play();
        }
    }

    replay(): void {
        if (!this.liveFeedPaused) {
            this.play(this.call || this.callPrevious);
        }
    }

    searchCalls(options: RdioScannerSearchOptions): void {
        this.wsSend(WsCommand.ListCall, options);
    }

    skip(nodelay = false): void {
        this.stop(!this.callQueue.length);

        if (nodelay) {
            if (this.skipDelay) {
                this.skipDelay = clearTimeout(this.skipDelay);
            }

            this.play();

        } else {
            this.skipDelay = setTimeout(() => {
                this.skipDelay = undefined;

                this.play();
            }, 1000);
        }
    }

    stop(emitEvent = true): void {
        if (this.audioSource) {
            if (this.audioTimer !== null) {
                clearInterval(this.audioTimer);
            }

            this.audioSource.onended = null;
            this.audioSource.stop();
            this.audioSource.disconnect();
            this.audioSource = null;

            this.audioStartTime = NaN;

            this.audioTimer = null;
        }

        if (this.call) {
            this.callPrevious = this.call;

            this.call = null;

            if (emitEvent) {
                this.event.emit({ call: null });
            }
        }
    }

    toggleGroup(label: string): void {
        const group = this.groups.find((gr) => gr.label === label);

        if (group) {
            if (this.liveFeedMapPriorToHoldSystem) {
                this.liveFeedMapPriorToHoldSystem = null;
            }

            if (this.liveFeedMapPriorToHoldTalkgroup) {
                this.liveFeedMapPriorToHoldTalkgroup = null;
            }

            const status = group.status === RdioScannerGroupStatus.On ? false : true;

            this.config.systems.forEach((sys) => {
                sys.talkgroups.forEach((tg) => {
                    if (tg.group === label) {
                        this.liveFeedMap[sys.id][tg.id] = status;
                    }
                });
            });

            this.buildGroups();

            if (this.call && !this.liveFeedMap[this.call.system] &&
                this.liveFeedMap[this.call.system][this.call.talkgroup]) {

                this.skip();
            }

            if (this.liveFeedActive) {
                this.liveFeedStart();
            }

            this.liveFeedStore();

            this.cleanQueue();

            this.event.emit({
                groups: this.groups,
                holdSys: false,
                holdTg: false,
                map: this.liveFeedMap,
                queue: this.callQueue.length,
            });
        }
    }

    private bootstrapAudio(): void {
        const events = ['mousedown', 'touchdown'];

        const bootstrap = () => {
            if (!this.audioContext) {
                if ('webkitAudioContext' in window) {
                    this.audioContext = new webkitAudioContext();

                } else {
                    this.audioContext = new AudioContext();
                }
            }

            if (this.audioContext) {
                this.audioContext.resume().then(() => {
                    events.forEach((event) => document.body.removeEventListener(event, bootstrap));
                });
            }
        };

        events.forEach((event) => document.body.addEventListener(event, bootstrap));
    }

    private buildGroups(): void {
        if (this.config?.useGroup) {
            this.groups = this.config.systems.reduce((groups, system) => {
                system.talkgroups.forEach((talkgroup) => {
                    if (!groups.find((group) => group.label === talkgroup.group)) {
                        const allOn = this.config.systems.every((sys) => {
                            return sys.talkgroups.filter((tg) => tg.group === talkgroup.group)
                                .every((tg) => this.liveFeedMap[sys.id][tg.id]);
                        });

                        const allOff = this.config.systems.every((sys) => {
                            return sys.talkgroups.filter((tg) => tg.group === talkgroup.group)
                                .every((tg) => !this.liveFeedMap[sys.id][tg.id]);
                        });

                        const label = talkgroup.group;

                        const status = allOn ? RdioScannerGroupStatus.On
                            : allOff ? RdioScannerGroupStatus.Off
                                : RdioScannerGroupStatus.Partial;

                        groups.push({ label, status });
                    }
                });

                return groups;
            }, [] as RdioScannerGroup[]).sort((a, b) => a.label.localeCompare(b.label));
        }
    }

    private cleanQueue(): void {
        this.callQueue = this.callQueue.filter((call: RdioScannerCall) => {
            return this.liveFeedMap && this.liveFeedMap[call.system] && this.liveFeedMap[call.system][call.talkgroup];
        });
    }

    private download(call: RdioScannerCall): void {
        const file = call.audio.data.reduce((str, val) => str += String.fromCharCode(val), '');
        const fileName = call.audioName || 'unknown.dat';
        const fileType = call.audioType || 'audio/*';
        const fileUri = `data:${fileType};base64,${btoa(file)}`;

        const el = this.document.createElement('a');

        el.style.display = 'none';

        el.setAttribute('href', fileUri);
        el.setAttribute('download', fileName);

        this.document.body.appendChild(el);

        el.click();

        this.document.body.removeChild(el);
    }

    private getCall(id: string, flags?: any): void {
        this.wsSend(WsCommand.Call, id, flags);
    }

    private liveFeedRebuild(): void {
        if (Array.isArray(this.config && this.config.systems)) {
            this.liveFeedMap = this.config.systems.reduce((sysMap, sys) => {
                const tgs = sys.talkgroups.map((tg) => tg.id.toString());

                sysMap[sys.id] = sys.talkgroups.reduce((tgMap, tg) => {
                    const state = this.liveFeedMap && this.liveFeedMap[sys.id] && this.liveFeedMap[sys.id][tg.id];

                    tgMap[tg.id] = typeof state === 'boolean' ? state : true;

                    return tgMap;
                }, sysMap[sys.id] || {});

                return sysMap;
            }, {} as RdioScannerLiveFeedMap);

            this.liveFeedStore();

            this.buildGroups();
        }
    }

    private liveFeedRestore(): any {
        if (window instanceof Window && window.localStorage instanceof Storage) {
            try {
                this.liveFeedMap = JSON.parse(window.localStorage.getItem(AppRdioScannerService.LOCAL_STORAGE_KEY));

            } catch (err) {
                this.liveFeedMap = {};
            }
        }
    }

    private liveFeedStart(): void {
        this.wsSend(WsCommand.LiveFeedMap, this.liveFeedMap);
    }

    private liveFeedStop(): void {
        this.wsSend(WsCommand.LiveFeedMap, null);

        this.stop();
    }

    private liveFeedStore(): void {
        if (window instanceof Window && window.localStorage instanceof Storage) {
            window.localStorage.setItem(AppRdioScannerService.LOCAL_STORAGE_KEY, JSON.stringify(this.liveFeedMap));
        }
    }

    private messageParser(message): void {
        try {
            message = JSON.parse(message);

        } catch (_) {
            console.warn('Invalid control message received');
        }

        if (Array.isArray(message)) {
            switch (message[0]) {
                case WsCommand.Call:
                    switch (message[2]) {
                        case WsCallFlag.Download:
                            this.download(message[1]);
                            break;

                        case WsCallFlag.Play:
                            this.play(message[1]);
                            break;

                        default:
                            this.queue(message[1]);
                    }

                    break;

                case WsCommand.Config:
                    this.config = message[1];

                    if ('systems' in this.config) {
                        this.liveFeedRebuild();
                    }

                    if (this.liveFeedActive) {
                        this.liveFeedStart();
                    }

                    this.event.emit({
                        auth: false,
                        config: this.config,
                        groups: this.groups,
                        holdSys: !!this.liveFeedMapPriorToHoldSystem,
                        holdTg: !!this.liveFeedMapPriorToHoldTalkgroup,
                        map: this.liveFeedMap,
                    });

                    break;

                case WsCommand.ListCall:
                    this.event.emit({ list: message[1] });

                    break;

                case WsCommand.LiveFeedMap:
                    this.liveFeedActive = message[1];

                    if (this.liveFeedActive) {
                        this.event.emit({ liveFeed: this.liveFeedActive });

                    } else {
                        this.callQueue.splice(0, this.callQueue.length);

                        this.event.emit({
                            liveFeed: this.liveFeedActive,
                            queue: this.callQueue.length,
                        });
                    }

                    break;

                case WsCommand.Pin:
                    this.event.emit({ auth: true });

                    break;
            }
        }
    }

    private wsClose(): void {
        if (this.webSocket instanceof WebSocket) {
            this.webSocket.onclose = undefined;
            this.webSocket.onerror = undefined;
            this.webSocket.onmessage = undefined;
            this.webSocket.onopen = undefined;

            this.webSocket.close();

            this.webSocket = undefined;
        }
    }

    private wsOpen(): void {
        const wsUrl = window.origin.replace(/^http/, 'ws');

        this.webSocket = new WebSocket(wsUrl);

        this.webSocket.onclose = (ev: CloseEvent) => {
            if (ev.code !== 1000) {
                this.wsReconnect();
            }
        };

        this.webSocket.onerror = () => { };

        this.webSocket.onopen = () => {
            this.webSocket.onmessage = (ev: MessageEvent) => this.messageParser(ev.data);

            this.wsSend(WsCommand.Config);
        };
    }

    private wsReconnect(): void {
        this.wsClose();

        setTimeout(() => this.wsOpen(), 5 * 1000);
    }

    private wsSend(command: string, payload?: any, flags?: any): void {
        if (this.webSocket instanceof WebSocket && this.webSocket.readyState === 1) {
            const message = [command, payload];

            if (flags !== null && flags !== undefined) {
                message.push(flags);
            }

            this.webSocket.send(JSON.stringify(message));
        }
    }
}
