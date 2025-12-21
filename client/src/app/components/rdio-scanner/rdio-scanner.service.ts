/*
 * *****************************************************************************
 * Copyright (C) 2019-2026 Chrystian Huot <chrystian@huot.qc.ca>
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


import { EventEmitter, Inject, Injectable, OnDestroy, DOCUMENT } from '@angular/core';
import { Router } from '@angular/router';
import { interval, Subscription, timer } from 'rxjs';
import { takeWhile } from 'rxjs/operators';
import { AppUpdateService } from '../../shared/update/update.service';
import {
    RdioScannerAvoidOptions,
    RdioScannerBeepStyle,
    RdioScannerCall,
    RdioScannerCategory,
    RdioScannerCategoryStatus,
    RdioScannerCategoryType,
    RdioScannerConfig,
    RdioScannerEvent,
    RdioScannerLivefeed,
    RdioScannerLivefeedMap,
    RdioScannerLivefeedMode,
    RdioScannerOscillatorData,
    RdioScannerPlaybackList,
    RdioScannerSearchOptions,
} from './rdio-scanner';

declare global {
    interface Window {
        webkitAudioContext: typeof AudioContext;
    }
}

enum WebsocketCallFlag {
    Download = 'd',
    Play = 'p',
}

enum WebsocketCommand {
    Call = 'CAL',
    Config = 'CFG',
    Expired = 'XPR',
    ListCall = 'LCL',
    ListenersCount = 'LSC',
    LivefeedMap = 'LFM',
    Max = 'MAX',
    Pin = 'PIN',
    Version = 'VER',
}

@Injectable()
export class RdioScannerService implements OnDestroy {
    static LOCAL_STORAGE_KEY_LEGACY = 'rdio-scanner';
    static LOCAL_STORAGE_KEY_LFM = 'rdio-scanner-lfm';
    static LOCAL_STORAGE_KEY_PIN = 'rdio-scanner-pin';

    event = new EventEmitter<RdioScannerEvent>();

    private audioContext: AudioContext | undefined;

    private audioSource: AudioBufferSourceNode | undefined;
    private audioSourceStartTime = NaN;

    private call: RdioScannerCall | undefined;
    private callPrevious: RdioScannerCall | undefined;
    private callQueue: RdioScannerCall[] = [];

    private categories: RdioScannerCategory[] = [];

    private config: RdioScannerConfig = {
        dimmerDelay: false,
        groups: {},
        groupsData: [],
        keypadBeeps: undefined,
        playbackGoesLive: false,
        showListenersCount: false,
        systems: [],
        tags: {},
        tagsData: [],
        time12hFormat: false,
    };

    private instanceId = 'default';

    private livefeedMap = {} as RdioScannerLivefeedMap;
    private livefeedMapPriorToHoldSystem: RdioScannerLivefeedMap | undefined;
    private livefeedMapPriorToHoldTalkgroup: RdioScannerLivefeedMap | undefined;
    private livefeedMode = RdioScannerLivefeedMode.Offline;
    private livefeedPaused = false;

    private oscillatorContext: AudioContext | undefined;

    private playbackList: RdioScannerPlaybackList | undefined;
    private playbackPending: number | undefined;
    private playbackRefreshing = false;

    private skipDelay: Subscription | undefined;

    private websocket: WebSocket | undefined;

    constructor(
        appUpdateService: AppUpdateService,
        private router: Router,
        @Inject(DOCUMENT) private document: Document,
    ) {
        if (router.url.endsWith('/reset')) {
            window?.localStorage?.clear();

            router.navigateByUrl(router.url.replace('/reset', ''), {
                replaceUrl: true,
            }).then(() => window?.location?.reload());

            return;
        }

        this.bootstrapAudio();

        this.initializeInstanceId();

        this.readLivefeedMap();

        this.openWebsocket();
    }

    authenticate(password: string): void {
        this.sendtoWebsocket(WebsocketCommand.Pin, window.btoa(password));
    }

    avoid(options: RdioScannerAvoidOptions = {}): void {
        const clearTimer = (lfm: RdioScannerLivefeed): void => {
            lfm.minutes = undefined;
            lfm.timer?.unsubscribe();
            lfm.timer = undefined;
        };

        const setTimer = (lfm: RdioScannerLivefeed, minutes: number): void => {
            lfm.minutes = minutes;
            lfm.timer = timer(minutes * 60 * 1000).subscribe(() => {
                lfm.active = true;
                lfm.minutes = undefined;
                lfm.timer = undefined;

                this.rebuildCategories();
                this.saveLivefeedMap();

                this.event.emit({
                    categories: this.categories,
                    map: this.livefeedMap,
                });
            });
        };

        if (this.livefeedMapPriorToHoldSystem) {
            this.livefeedMapPriorToHoldSystem = undefined;
        }

        if (this.livefeedMapPriorToHoldTalkgroup) {
            this.livefeedMapPriorToHoldTalkgroup = undefined;
        }

        if (typeof options.all === 'boolean') {
            Object.keys(this.livefeedMap).map((sys: string) => +sys).forEach((sys: number) => {
                Object.keys(this.livefeedMap[sys]).map((tg: string) => +tg).forEach((tg: number) => {
                    const lfm = this.livefeedMap[sys][tg];
                    clearTimer(lfm);
                    lfm.active = typeof options.status === 'boolean' ? options.status : !!options.all;
                });
            });

        } else if (options.call) {
            const lfm = this.livefeedMap[options.call.system][options.call.talkgroup];
            clearTimer(lfm);
            lfm.active = typeof options.status === 'boolean' ? options.status : !lfm.active;
            if (typeof options.minutes === 'number') setTimer(lfm, options.minutes);

        } else if (options.system && options.talkgroup) {
            const lfm = this.livefeedMap[options.system.id][options.talkgroup.id];
            clearTimer(lfm);
            lfm.active = typeof options.status === 'boolean' ? options.status : !lfm.active;
            if (typeof options.minutes === 'number') setTimer(lfm, options.minutes);

        } else if (options.system && !options.talkgroup) {
            const sys = options.system.id;
            Object.keys(this.livefeedMap[sys]).map((tg: string) => +tg).forEach((tg: number) => {
                const lfm = this.livefeedMap[sys][tg];
                clearTimer(lfm);
                lfm.active = typeof options.status === 'boolean' ? options.status : !lfm.active;
            });

        } else {
            const call = this.call || this.callPrevious;
            if (call) {
                const lfm = this.livefeedMap[call.system][call.talkgroup];
                clearTimer(lfm);
                lfm.active = typeof options.status === 'boolean' ? options.status : !lfm.active;
                if (typeof options.minutes === 'number') setTimer(lfm, options.minutes);
            }
        }

        if (this.livefeedMode !== RdioScannerLivefeedMode.Playback) {
            this.cleanQueue();
        }

        this.rebuildCategories();

        this.saveLivefeedMap();

        if (this.livefeedMode === RdioScannerLivefeedMode.Online) {
            this.startLivefeed();
        }

        this.event.emit({
            categories: this.categories,
            holdSys: false,
            holdTg: false,
            map: this.livefeedMap,
            queue: this.callQueue.length,
        });
    }

    async beep(style = RdioScannerBeepStyle.Activate): Promise<void> {
        const seq = this.config.keypadBeeps && this.config.keypadBeeps[style];

        if (seq) await this.playOscillatorSequence(seq);
    }

    clearPin(): void {
        window?.localStorage.removeItem(RdioScannerService.LOCAL_STORAGE_KEY_PIN);
    }

    holdSystem(options?: { resubscribe?: boolean }): void {
        const call = this.call || this.callPrevious;

        if (call && this.livefeedMap) {
            if (this.livefeedMapPriorToHoldSystem) {
                this.livefeedMap = this.livefeedMapPriorToHoldSystem;

                this.livefeedMapPriorToHoldSystem = undefined;

            } else {
                if (this.livefeedMapPriorToHoldTalkgroup) {
                    this.holdTalkgroup({ resubscribe: false });
                }

                this.livefeedMapPriorToHoldSystem = this.livefeedMap;

                this.livefeedMap = Object.keys(this.livefeedMap).map((sys) => +sys).reduce((sysMap, sys) => {
                    const allOn = Object.keys(this.livefeedMap[sys]).map((tg) => +tg).every((tg) => !this.livefeedMap[sys][tg]);

                    sysMap[sys] = Object.keys(this.livefeedMap[sys]).map((tg) => +tg).reduce((tgMap, tg) => {
                        this.livefeedMap[sys][tg].timer?.unsubscribe();

                        tgMap[tg] = {
                            active: sys === call.system ? allOn || this.livefeedMap[sys][tg].active : false,
                        } as RdioScannerLivefeed;

                        return tgMap;
                    }, {} as { [key: number]: RdioScannerLivefeed });

                    return sysMap;
                }, {} as RdioScannerLivefeedMap);

                this.cleanQueue();
            }

            this.rebuildCategories();

            if (typeof options?.resubscribe !== 'boolean' || options.resubscribe) {
                if (this.livefeedMode === RdioScannerLivefeedMode.Online) {
                    this.startLivefeed();
                }
            }

            this.event.emit({
                categories: this.categories,
                holdSys: !!this.livefeedMapPriorToHoldSystem,
                holdTg: false,
                map: this.livefeedMap,
                queue: this.callQueue.length,
            });
        }
    }

    holdTalkgroup(options?: { resubscribe?: boolean }): void {
        const call = this.call || this.callPrevious;

        if (call && this.livefeedMap) {
            if (this.livefeedMapPriorToHoldTalkgroup) {
                this.livefeedMap = this.livefeedMapPriorToHoldTalkgroup;

                this.livefeedMapPriorToHoldTalkgroup = undefined;

            } else {
                if (this.livefeedMapPriorToHoldSystem) {
                    this.holdSystem({ resubscribe: false });
                }

                this.livefeedMapPriorToHoldTalkgroup = this.livefeedMap;

                this.livefeedMap = Object.keys(this.livefeedMap).map((sys) => +sys).reduce((sysMap, sys) => {
                    sysMap[sys] = Object.keys(this.livefeedMap[sys]).map((tg) => +tg).reduce((tgMap, tg) => {
                        this.livefeedMap[sys][tg].timer?.unsubscribe();

                        tgMap[tg] = {
                            active: sys === call.system ? tg === call.talkgroup : false,
                        } as RdioScannerLivefeed;

                        return tgMap;
                    }, {} as { [key: number]: RdioScannerLivefeed });

                    return sysMap;
                }, {} as RdioScannerLivefeedMap);

                this.cleanQueue();
            }

            this.rebuildCategories();

            if (typeof options?.resubscribe !== 'boolean' || options.resubscribe) {
                if (this.livefeedMode === RdioScannerLivefeedMode.Online) {
                    this.startLivefeed();
                }
            }

            this.event.emit({
                categories: this.categories,
                holdSys: false,
                holdTg: !!this.livefeedMapPriorToHoldTalkgroup,
                map: this.livefeedMap,
                queue: this.callQueue.length,
            });
        }
    }

    isAvoided(call: RdioScannerCall): boolean {
        return !!this.livefeedMap[call.system] && this.livefeedMap[call.system][call.talkgroup]?.active !== true;
    }

    isAvoidedTimer(call: RdioScannerCall): number {
        if (!!this.livefeedMap[call.system] && this.livefeedMap[call.system][call.talkgroup]?.minutes !== undefined) {
            return this.livefeedMap[call.system][call.talkgroup]?.minutes || 0;
        }
        return 0;
    }

    isPatched(call: RdioScannerCall): boolean {
        return this.isAvoided(call) && call.patches?.some((tg) => {
            return !!this.livefeedMap[call.system] && this.livefeedMap[call.system][tg]?.active || false;
        });
    }

    livefeed(): void {
        if (this.livefeedMode === RdioScannerLivefeedMode.Offline) {
            this.startLivefeed();

        } else if (this.livefeedMode === RdioScannerLivefeedMode.Online) {
            this.stopLivefeed();

        } else if (this.livefeedMode === RdioScannerLivefeedMode.Playback) {
            this.stopPlaybackMode();
        }
    }

    loadAndDownload(id: number): void {
        if (!id) {
            return;
        }

        this.getCall(id, WebsocketCallFlag.Download);
    }

    loadAndPlay(id: number): void {
        if (!id) {
            return;
        }

        if (this.skipDelay) {
            this.skipDelay.unsubscribe();

            this.skipDelay = undefined;
        }

        this.playbackPending = id;

        this.stop();

        if (this.livefeedMode === RdioScannerLivefeedMode.Offline) {
            this.livefeedMode = RdioScannerLivefeedMode.Playback;

            if (this.livefeedMapPriorToHoldSystem) {
                this.holdSystem({ resubscribe: false });
            }

            if (this.livefeedMapPriorToHoldTalkgroup) {
                this.holdTalkgroup({ resubscribe: false });
            }

            this.event.emit({ livefeedMode: this.livefeedMode, playbackPending: id });

        } else if (this.livefeedMode === RdioScannerLivefeedMode.Playback) {
            this.event.emit({ playbackPending: id });
        }

        this.getCall(id, WebsocketCallFlag.Play);
    }

    ngOnDestroy(): void {
        this.closeWebsocket();

        this.stop();
    }

    pause(status = !this.livefeedPaused): void {
        this.livefeedPaused = status;

        if (status) {
            this.audioContext?.suspend();

        } else {
            this.audioContext?.resume();

            this.play();
        }

        this.event.emit({ pause: this.livefeedPaused });
    }

    play(call?: RdioScannerCall | undefined): void {
        if (this.livefeedPaused || this.skipDelay) {
            return;

        } else if (call?.audio) {
            if (this.call) {
                this.stop({ emit: false });
            }

            this.call = call;

        } else if (this.call) {
            return;

        } else {
            this.call = this.callQueue.shift();
        }

        if (!this.call?.audio) {
            return;
        }

        const queue = this.livefeedMode === RdioScannerLivefeedMode.Playback
            ? this.getPlaybackQueueCount()
            : this.callQueue.length;

        const arrayBuffer = new ArrayBuffer(this.call.audio.data.length);
        const arrayBufferView = new Uint8Array(arrayBuffer);

        for (let i = 0; i < (this.call.audio.data.length); i++) {
            arrayBufferView[i] = this.call.audio.data[i];
        }

        this.audioContext?.decodeAudioData(arrayBuffer, async (buffer) => {
            if (!this.audioContext || this.audioSource || !this.call) {
                return;
            }

            await this.playAlert(this.call);

            this.audioSource = this.audioContext.createBufferSource();
            this.audioSource.buffer = buffer;
            this.audioSource.connect(this.audioContext.destination);
            this.audioSource.onended = () => this.skip({ delay: true });
            this.audioSource.start();

            this.event.emit({ call: this.call, queue });

            interval(500).pipe(takeWhile(() => !!this.call)).subscribe(() => {
                if (this.audioContext && !isNaN(this.audioContext.currentTime)) {
                    if (isNaN(this.audioSourceStartTime)) {
                        this.audioSourceStartTime = this.audioContext.currentTime;
                    }

                    if (!this.livefeedPaused) {
                        this.event.emit({ time: this.audioContext.currentTime - this.audioSourceStartTime });
                    }
                }
            });
        }, () => {
            this.event.emit({ call: this.call, queue });

            this.skip({ delay: false });
        });
    }

    queue(call: RdioScannerCall, options?: { priority?: boolean }): void {
        if (!call?.audio || this.livefeedMode === RdioScannerLivefeedMode.Offline) {
            return;
        }

        if (options?.priority) {
            this.callQueue.unshift(call);

        } else {
            this.callQueue.push(call);
        }

        if (this.audioSource || this.call || this.livefeedPaused || this.skipDelay) {
            this.event.emit({
                queue: this.livefeedMode === RdioScannerLivefeedMode.Online ? this.callQueue.length : this.getPlaybackQueueCount(),
            });

        } else {
            this.play();
        }
    }

    replay(): void {
        this.play(this.call || this.callPrevious);
    }

    readPin(): string | undefined {
        const pin = window?.localStorage?.getItem(RdioScannerService.LOCAL_STORAGE_KEY_PIN);

        return pin ? window.atob(pin) : undefined;
    }

    savePin(pin: string): void {
        window?.localStorage?.setItem(RdioScannerService.LOCAL_STORAGE_KEY_PIN, window.btoa(pin));
    }

    searchCalls(options: RdioScannerSearchOptions): void {
        this.sendtoWebsocket(WebsocketCommand.ListCall, options);
    }

    skip(options?: { delay?: boolean }): void {
        const play = () => {
            if (this.livefeedMode === RdioScannerLivefeedMode.Playback) {
                this.playbackNextCall();

            } else {
                this.play();
            }
        };

        this.stop();

        if (options?.delay) {
            this.skipDelay = timer(1000).subscribe(() => {
                this.skipDelay = undefined;

                play();
            });

        } else {
            if (this.skipDelay) {
                this.skipDelay?.unsubscribe();

                this.skipDelay = undefined;
            }

            play();
        }
    }

    startLivefeed(): void {
        const lfm = Object.keys(this.livefeedMap).reduce((sysMap: { [key: number]: { [key: number]: boolean } }, sys) => {
            sysMap[+sys] = Object.keys(this.livefeedMap[+sys]).reduce((tgMap: { [key: number]: boolean }, tg: string) => {
                tgMap[+tg] = this.livefeedMap[+sys][+tg].active;
                return tgMap;
            }, {});
            return sysMap;
        }, {});

        this.livefeedMode = RdioScannerLivefeedMode.Online;

        this.event.emit({ livefeedMode: this.livefeedMode });

        this.sendtoWebsocket(WebsocketCommand.LivefeedMap, lfm);
    }

    stop(options?: { emit?: boolean }): void {
        if (this.audioSource) {
            this.audioSource.onended = null;
            this.audioSource.stop();
            this.audioSource.disconnect();
            this.audioSource = undefined;
            this.audioSourceStartTime = NaN;
        }

        if (this.call) {
            this.callPrevious = this.call;

            this.call = undefined;
        }

        if (typeof options?.emit !== 'boolean' || options.emit) {
            this.event.emit({ call: this.call });
        }
    }

    stopLivefeed(): void {
        this.livefeedMode = RdioScannerLivefeedMode.Offline;

        this.clearQueue();

        this.event.emit({ livefeedMode: this.livefeedMode, queue: 0 });

        this.stop();

        this.sendtoWebsocket(WebsocketCommand.LivefeedMap, null);
    }

    stopPlaybackMode(): void {
        this.livefeedMode = RdioScannerLivefeedMode.Offline;

        this.playbackRefreshing = false;

        this.clearQueue();

        this.event.emit({ livefeedMode: this.livefeedMode, queue: 0 });

        this.stop();
    }

    toggleCategory(category: RdioScannerCategory): void {
        const clearTimer = (lfm: RdioScannerLivefeed): void => {
            lfm.minutes = 0;
            lfm.timer?.unsubscribe();
            lfm.timer = undefined;
        };

        if (category) {
            if (this.livefeedMapPriorToHoldSystem) {
                this.livefeedMapPriorToHoldSystem = undefined;
            }

            if (this.livefeedMapPriorToHoldTalkgroup) {
                this.livefeedMapPriorToHoldTalkgroup = undefined;
            }

            const status = category.status === RdioScannerCategoryStatus.On ? false : true;

            this.config?.systems.forEach((sys) => {
                sys.talkgroups?.forEach((tg) => {
                    const lfm = this.livefeedMap[sys.id][tg.id];

                    if (category.type == RdioScannerCategoryType.Group && tg.groups.includes(category.label)) {
                        clearTimer(lfm);
                        lfm.active = status;

                    } else if (category.type == RdioScannerCategoryType.Tag && tg.tag === category.label) {
                        clearTimer(lfm);
                        lfm.active = status;
                    }
                });
            });

            this.rebuildCategories();

            if (this.call && !this.livefeedMap[this.call.system] && this.livefeedMap[this.call.system][this.call.talkgroup]) {
                clearTimer(this.livefeedMap[this.call.system][this.call.talkgroup]);
                this.skip();
            }

            if (this.livefeedMode === RdioScannerLivefeedMode.Online) {
                this.startLivefeed();
            }

            this.saveLivefeedMap();

            this.cleanQueue();

            this.event.emit({
                categories: this.categories,
                holdSys: false,
                holdTg: false,
                map: this.livefeedMap,
                queue: this.callQueue.length,
            });
        }
    }

    private bootstrapAudio(): void {
        const events = ['keydown', 'mousedown', 'touchstart'];

        const bootstrap = async () => {
            if (!this.audioContext) {
                this.audioContext = new (window.AudioContext || window.webkitAudioContext)({ latencyHint: 'playback' });
            }

            if (!this.oscillatorContext) {
                this.oscillatorContext = new (window.AudioContext || window.webkitAudioContext)({ latencyHint: 'interactive' });
            }

            if (this.audioContext) {
                const resume = () => {
                    if (!this.livefeedPaused) {
                        if (this.audioContext?.state === 'suspended') {
                            this.audioContext?.resume().then(() => resume());
                        }
                    }
                };

                await this.audioContext.resume();

                this.audioContext.onstatechange = () => resume();
            }

            if (this.oscillatorContext) {
                const resume = () => {
                    if (this.oscillatorContext?.state === 'suspended') {
                        this.oscillatorContext?.resume().then(() => resume());
                    }
                };

                await this.oscillatorContext.resume();

                this.oscillatorContext.onstatechange = () => resume();
            }

            if (this.audioContext && this.oscillatorContext) {
                events.forEach((event) => document.body.removeEventListener(event, bootstrap));
            }
        };

        events.forEach((event) => document.body.addEventListener(event, bootstrap));
    }

    private cleanQueue(): void {
        const isActive = (call: RdioScannerCall) => {
            const lfm = (sys: number, tg: number): boolean => this.livefeedMap && this.livefeedMap[sys] && this.livefeedMap[sys][tg]?.active;
            let active = lfm(call.system, call.talkgroup);
            if (!active && Array.isArray(call.patches)) {
                for (let i = 0; i < call.patches.length; i++) {
                    active = lfm(call.system, call.patches[i]);
                    if (active) {
                        break;
                    }
                }
            }
            return active;
        };

        this.callQueue = this.callQueue.filter((call: RdioScannerCall) => isActive(call));

        if (this.call && !isActive(this.call)) {
            this.skip();
        }
    }

    private clearQueue(): void {
        this.callQueue.splice(0, this.callQueue.length);
    }

    private closeWebsocket(): void {
        if (this.websocket instanceof WebSocket) {
            this.websocket.onclose = null;
            this.websocket.onerror = null;
            this.websocket.onmessage = null;
            this.websocket.onopen = null;

            this.websocket.close();

            this.websocket = undefined;
        }
    }

    private download(call: RdioScannerCall): void {
        if (call.audio) {
            const file = call.audio.data.reduce((str, val) => str += String.fromCharCode(val), '');
            const fileName = call.audioName || 'unknown.dat';
            const fileType = call.audioType || 'audio/*';
            const fileUri = `data:${fileType};base64,${window.btoa(file)}`;

            const el = this.document.createElement('a');

            el.style.display = 'none';

            el.setAttribute('href', fileUri);
            el.setAttribute('download', fileName);

            this.document.body.appendChild(el);

            el.click();

            this.document.body.removeChild(el);
        }
    }

    private getCall(id: number, flags?: WebsocketCallFlag): void {
        this.sendtoWebsocket(WebsocketCommand.Call, `${id}`, flags);
    }

    private getPlaybackQueueCount(id = this.call?.id || this.callPrevious?.id): number {
        let queueCount = 0;

        if (id && this.playbackList) {
            const index = this.playbackList.results.findIndex((call) => call.id === id);

            if (index !== -1) {
                if (this.playbackList.options.sort === -1) {
                    queueCount = this.playbackList.options.offset + index;

                } else {
                    queueCount = this.playbackList.count - this.playbackList.options.offset - index - 1;
                }
            }
        }

        return queueCount;
    }

    private initializeInstanceId(): void {
        this.instanceId = this.router.parseUrl(this.router.url).queryParams['id'] || this.instanceId;
    }

    private openWebsocket(): void {
        const websocketUrl = window.location.href.replace(/^http/, 'ws');

        this.websocket = new WebSocket(websocketUrl);

        this.websocket.onclose = (ev: CloseEvent) => {
            this.event.emit({ linked: false });

            if (ev.code !== 1000) {
                timer(2000).subscribe(() => this.reconnectWebsocket());
            }
        };

        this.websocket.onopen = () => {
            this.event.emit({ linked: true });

            if (this.websocket instanceof WebSocket) {
                this.websocket.onmessage = (ev: MessageEvent) => this.parseWebsocketMessage(ev.data);
            }

            this.sendtoWebsocket(WebsocketCommand.Version);
            this.sendtoWebsocket(WebsocketCommand.Config);
        };
    }

    private parseWebsocketMessage(message: string): void {
        try {
            message = JSON.parse(message);

        } catch (error) {
            console.warn(`Invalid control message received, ${error}`);
        }

        if (Array.isArray(message)) {
            switch (message[0]) {
                case WebsocketCommand.Call:
                    if (message[1] !== null) {
                        const call: RdioScannerCall = message[1];
                        const flag: string = message[2];

                        if (flag === WebsocketCallFlag.Download) {
                            this.download(message[1]);

                        } else if (flag === WebsocketCallFlag.Play && call.id === this.playbackPending) {
                            this.playbackPending = undefined;

                            this.queue(this.transformCall(call), { priority: true });

                        } else {
                            this.queue(this.transformCall(call));
                        }
                    }

                    break;

                case WebsocketCommand.Config: {
                    const config = message[1];

                    this.config = {
                        alerts: config.alerts,
                        branding: typeof config.branding === 'string' ? config.branding : '',
                        dimmerDelay: typeof config.dimmerDelay === 'number' ? config.dimmerDelay : 5000,
                        email: typeof config.email === 'string' ? config.email : '',
                        groups: typeof config.groups !== null && typeof config.groups === 'object' ? config.groups : {},
                        groupsData: Array.isArray(config.groupsData) ? config.groupsData : [],
                        keypadBeeps: config.keypadBeeps !== null && typeof config.keypadBeeps === 'object' ? config.keypadBeeps : {},
                        playbackGoesLive: typeof config.playbackGoesLive === 'boolean' ? config.playbackGoesLive : false,
                        showListenersCount: typeof config.showListenersCount === 'boolean' ? config.showListenersCount : false,
                        systems: Array.isArray(config.systems) ? config.systems.slice() : [],
                        tags: typeof config.tags !== null && typeof config.tags === 'object' ? config.tags : {},
                        tagsData: Array.isArray(config.tagsData) ? config.tagsData : [],
                        time12hFormat: typeof config.time12hFormat === 'boolean' ? config.time12hFormat : false,
                    };

                    this.rebuildLivefeedMap();

                    if (this.livefeedMode === RdioScannerLivefeedMode.Online) {
                        this.startLivefeed();
                    }

                    this.event.emit({
                        auth: false,
                        categories: this.categories,
                        config: this.config,
                        holdSys: !!this.livefeedMapPriorToHoldSystem,
                        holdTg: !!this.livefeedMapPriorToHoldTalkgroup,
                        map: this.livefeedMap,
                    });

                    break;
                }

                case WebsocketCommand.Expired:
                    this.event.emit({ auth: true, expired: true });

                    break;

                case WebsocketCommand.ListCall:
                    this.playbackList = message[1];

                    if (this.playbackList) {
                        this.playbackList.results = this.playbackList.results.map((call) => this.transformCall(call));

                        this.event.emit({ playbackList: this.playbackList });

                        if (this.livefeedMode === RdioScannerLivefeedMode.Playback) {
                            this.playbackNextCall();
                        }
                    }

                    break;

                case WebsocketCommand.ListenersCount:
                    this.event.emit({ listeners: message[1] });

                    break;

                case WebsocketCommand.Max:
                    this.event.emit({ auth: true, tooMany: true });

                    break;

                case WebsocketCommand.Pin:
                    this.event.emit({ auth: true });

                    break;

                case WebsocketCommand.Version: {
                    const data = message[1];

                    if (data !== null && typeof data === 'object') {
                        const branding = data['branding'];
                        const email = data['email'];

                        if (typeof branding === 'string') {
                            this.config.branding = branding;
                        }

                        if (typeof email === 'string') {
                            this.config.email = email;
                        }

                        if (this.config.branding || this.config.email) {
                            this.event.emit({ config: this.config });
                        }
                    }

                    break;
                }
            }
        }
    }

    private async playAlert(call: RdioScannerCall): Promise<void> {
        if (this.config.alerts) {
            let alert: string | undefined;

            call?.talkgroupData?.groups.some((label) => {
                const group = this.config.groupsData?.find((group) => group.label == label);

                if (group && group.alert) {
                    alert = group.alert;

                    return true;
                }

                return false;
            });

            if (!alert) {
                const tag = this.config.tagsData?.find((tag) => tag.label == call.talkgroupData?.tag);

                if (tag && tag.alert) alert = tag.alert;
            }

            if (!alert) alert = call.systemData?.alert;

            if (!alert) alert = call.talkgroupData?.alert;

            if (alert) await this.playOscillatorSequence(this.config.alerts[alert]);
            console.log(alert);
        }
    }

    private playbackNextCall(): void {
        if (this.call || this.livefeedMode !== RdioScannerLivefeedMode.Playback || !this.playbackList || this.playbackPending) {
            return;
        }

        const index = this.playbackList.results.findIndex((call) => call.id === this.callPrevious?.id);

        if (this.playbackList.options.sort === -1) {
            if (index === -1) {
                this.loadAndPlay(this.playbackList.results[this.playbackList.results.length - 1].id);

            } else if (index === 0) {
                if (this.playbackList.options.offset < this.playbackList.options.limit) {
                    if (this.playbackRefreshing) {
                        this.stopPlaybackMode();

                        if (this.config.playbackGoesLive) {
                            this.startLivefeed();
                        }

                    } else {
                        this.playbackRefreshing = true;
                        this.searchCalls(this.playbackList.options);
                    }

                } else {
                    this.searchCalls(Object.assign({}, this.playbackList.options, {
                        offset: this.playbackList.options.offset - this.playbackList.options.limit,
                    }));
                }

            } else {
                this.loadAndPlay(this.playbackList.results[index - 1].id);
            }

        } else {
            if (index === -1) {
                this.loadAndPlay(this.playbackList.results[0].id);

            } else if (index === this.playbackList.results.length - 1) {
                if (this.playbackList.options.offset < (this.playbackList.count - this.playbackList.options.limit)) {
                    this.searchCalls(Object.assign({}, this.playbackList.options, {
                        offset: this.playbackList.options.offset + this.playbackList.options.limit,
                    }));

                } else if (this.playbackRefreshing) {
                    this.stopPlaybackMode();

                    if (this.config.playbackGoesLive) {
                        this.startLivefeed();
                    }

                } else {
                    this.playbackRefreshing = true;
                    this.searchCalls(this.playbackList.options);
                }

            } else {
                this.loadAndPlay(this.playbackList.results[index + 1].id);
            }
        }
    }

    private playOscillatorSequence(seq: RdioScannerOscillatorData[]): Promise<void> {
        return new Promise((resolve) => {
            const context = this.oscillatorContext;

            if (!context || !seq) {
                resolve();

                return;
            }

            const gn = context.createGain();

            gn.gain.value = .1;

            gn.connect(context.destination);

            seq.forEach((data, index) => {
                const osc = context.createOscillator();

                osc.connect(gn);

                osc.frequency.value = data.frequency;

                osc.type = data.type;

                if (index === seq.length - 1) {
                    osc.onended = () => resolve();
                }

                osc.start(context.currentTime + data.begin);

                osc.stop(context.currentTime + data.end);
            });
        });
    }

    private readLivefeedMap(): void {
        try {
            let lfm: { [key: number]: { [key: number]: boolean } } = {};

            let store = window?.localStorage?.getItem(`${RdioScannerService.LOCAL_STORAGE_KEY_LFM}-${this.instanceId}`);

            if (store !== null) {
                lfm = JSON.parse(store);

            } else {
                store = window?.localStorage?.getItem(RdioScannerService.LOCAL_STORAGE_KEY_LEGACY);

                if (store !== null) {
                    lfm = JSON.parse(store);
                }
            }

            Object.keys(lfm ?? {}).forEach((sys: string) => {
                Object.keys(lfm[+sys]).forEach((tg) => {
                    if (!this.livefeedMap[+sys]) this.livefeedMap[+sys] = {};
                    if (!this.livefeedMap[+sys][+tg]) this.livefeedMap[+sys][+tg] = {} as RdioScannerLivefeed;
                    this.livefeedMap[+sys][+tg].active = lfm[+sys][+tg];
                });
            });

        } catch (_) {
            //
        }
    }

    private rebuildCategories(): void {
        this.categories = Object.keys(this.config.groups || []).map((label) => {
            const allOff = Object.keys(this.config.groups[label]).map((sys) => +sys)
                .every((sys: number) => this.config.groups[label] && this.config.groups[label][sys]
                    .every((tg) => this.livefeedMap[sys] && !this.livefeedMap[sys][tg].active));

            const allOn = Object.keys(this.config.groups[label]).map((sys) => +sys)
                .every((sys: number) => this.config.groups[label] && this.config.groups[label][sys]
                    .every((tg) => this.livefeedMap[sys] && this.livefeedMap[sys][tg].active));

            const status = allOff ? RdioScannerCategoryStatus.Off : allOn ? RdioScannerCategoryStatus.On : RdioScannerCategoryStatus.Partial;

            return { label, status, type: RdioScannerCategoryType.Group };
        })

        this.categories.sort((a, b) => a.label.localeCompare(b.label));
    }

    private rebuildLivefeedMap(): void {
        const lfm = this.config.systems.reduce((sysMap, sys) => {
            sysMap[sys.id] = sys.talkgroups.reduce((tgMap, tg) => {
                const group = this.categories.find((cat) => tg.groups.includes(cat.label));
                const tag = this.categories.find((cat) => cat.label === tg.tag);

                tgMap[tg.id] = (this.livefeedMap[sys.id] && this.livefeedMap[sys.id][tg.id])
                    ? this.livefeedMap[sys.id][tg.id]
                    : {
                        active: !(group?.status === RdioScannerCategoryStatus.Off || tag?.status === RdioScannerCategoryStatus.Off),
                    } as RdioScannerLivefeed;

                return tgMap;
            }, sysMap[sys.id] || {} as { [key: number]: RdioScannerLivefeed });
            return sysMap;
        }, {} as RdioScannerLivefeedMap);

        if (this.livefeedMapPriorToHoldSystem != null) {
            this.livefeedMapPriorToHoldSystem = lfm;
        } else if (this.livefeedMapPriorToHoldTalkgroup != null) {
            this.livefeedMapPriorToHoldTalkgroup = lfm;
        } else {
            this.livefeedMap = lfm;
        }

        this.saveLivefeedMap();

        this.rebuildCategories();
    }

    private reconnectWebsocket(): void {
        this.closeWebsocket();

        this.openWebsocket();
    }

    private saveLivefeedMap(): void {
        const lfm = Object.keys(this.livefeedMap).reduce((sysMap: { [key: number]: { [key: number]: boolean } }, sys: string) => {
            sysMap[+sys] = Object.keys(this.livefeedMap[+sys]).reduce((tgMap: { [key: number]: boolean }, tg: string) => {
                tgMap[+tg] = this.livefeedMap[+sys][+tg].active;
                return tgMap;
            }, {});
            return sysMap;
        }, {});

        window?.localStorage?.setItem(`${RdioScannerService.LOCAL_STORAGE_KEY_LFM}-${this.instanceId}`, JSON.stringify(lfm));
    }

    private sendtoWebsocket(command: string, payload?: unknown, flags?: string): void {
        if (this.websocket?.readyState === 1) {
            const message: unknown[] = [command];

            if (payload) {
                message.push(payload);
            }

            if (flags !== null && flags !== undefined) {
                message.push(flags);
            }

            this.websocket.send(JSON.stringify(message));
        }
    }


    private transformCall(call: RdioScannerCall): RdioScannerCall {
        if (call && Array.isArray(this.config?.systems)) {
            call.systemData = this.config.systems.find((system) => system.id === call.system);

            if (Array.isArray(call.systemData?.talkgroups)) {
                call.talkgroupData = call.systemData?.talkgroups.find((talkgroup) => talkgroup.id === call.talkgroup);
            }

            if (call.talkgroupData?.frequency) {
                call.frequency = call.talkgroupData.frequency;
            }

            call.groupsData = this.config.groupsData.filter((gd) => call.talkgroupData?.groups.some((l) => l === gd.label));

            call.tagData = this.config.tagsData.find((td) => td.label === call.talkgroupData?.tag);
        }

        return call;
    }
}
