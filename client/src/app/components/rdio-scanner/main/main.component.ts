/*
 * *****************************************************************************
 * Copyright (C) 2019-2022 Chrystian Huot <chrystian.huot@saubeo.solutions>
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

import { ChangeDetectorRef, Component, EventEmitter, OnDestroy, OnInit, Output, ViewChild } from '@angular/core';
import { FormBuilder } from '@angular/forms';
import { MatInput } from '@angular/material/input';
import { Subscription, timer } from 'rxjs';
import packageInfo from '../../../../../package.json';
import {
    RdioScannerAvoidOptions,
    RdioScannerBeepStyle,
    RdioScannerCall,
    RdioScannerConfig,
    RdioScannerEvent,
    RdioScannerLivefeedMap,
    RdioScannerLivefeedMode,
} from '../rdio-scanner';
import { RdioScannerService } from '../rdio-scanner.service';

const LOCAL_STORAGE_KEY = RdioScannerService.LOCAL_STORAGE_KEY + '-pin';

@Component({
    selector: 'rdio-scanner-main',
    styleUrls: [
        '../common.scss',
        './main.component.scss',
    ],
    templateUrl: './main.component.html',
})
export class RdioScannerMainComponent implements OnDestroy, OnInit {
    auth = false;
    authForm = this.ngFormBuilder.group({ password: [] });

    avoided = false;

    call: RdioScannerCall | undefined;
    callError = '0';
    callFrequency: string = this.formatFrequency(0);
    callHistory: RdioScannerCall[] = new Array<RdioScannerCall>(5);
    callPrevious: RdioScannerCall | undefined;
    callProgress = new Date(0, 0, 0, 0, 0, 0);
    callQueue = 0;
    callSpike = '0';
    callSystem = 'System';
    callTag = 'Tag';
    callTalkgroup = 'Talkgroup';
    callTalkgroupId = '0';
    /*
        * BEGIN OF RED TAPE:
        * 
        * By modifying, deleting or disabling the following lines, you harm
        * the open source project and its author.  Rdio Scanner represents a lot of
        * investment in time, support, testing and hardware.
        * 
        * Be respectful, sponsor the project if you can, use native apps when possible.
        * 
        */
    callTalkgroupName = `Rdio Scanner v${packageInfo.version}`;
    /**
     * END OF RED TAPE.
     */
    callTime = 0;
    callUnit = '0';

    clock = new Date();

    dimmer = false;

    holdSys = false;
    holdTg = false;

    ledStyle = '';

    linked = false;

    listeners = 0;

    livefeedOffline = true;
    livefeedOnline = false;
    livefeedPaused = false;

    map: RdioScannerLivefeedMap = {};

    patched = false;

    playbackMode = false;

    replayOffset = 0;
    replayTimer: Subscription | undefined;

    get showListenersCount(): boolean {
        return this.config?.showListenersCount || false;
    }

    @Output() openSearchPanel = new EventEmitter<void>();

    @Output() openSelectPanel = new EventEmitter<void>();

    @Output() toggleFullscreen = new EventEmitter<void>();

    @ViewChild('password', { read: MatInput }) private authPassword: MatInput | undefined;

    private clockTimer: Subscription | undefined;

    private config: RdioScannerConfig | undefined;

    private dimmerTimer: Subscription | undefined;

    private eventSubscription = this.rdioScannerService.event.subscribe((event: RdioScannerEvent) => this.eventHandler(event));

    constructor(
        private rdioScannerService: RdioScannerService,
        private ngChangeDetectorRef: ChangeDetectorRef,
        private ngFormBuilder: FormBuilder,
    ) { }

    authenticate(password = this.authForm.value.password): void {
        this.authForm.disable();

        this.rdioScannerService.authenticate(password);
    }

    authFocus(): void {
        if (this.auth && this.authPassword instanceof MatInput) {
            this.authPassword.focus();
        }
    }

    avoid(options?: RdioScannerAvoidOptions): void {
        if (this.auth) {
            this.authFocus();

        } else {
            const call = this.call || this.callPrevious;

            if (options || call) {
                this.rdioScannerService.avoid(options);

                if (call && !this.map[call.system][call.talkgroup]) {
                    this.rdioScannerService.beep(RdioScannerBeepStyle.Activate);

                } else {
                    this.rdioScannerService.beep(RdioScannerBeepStyle.Deactivate);
                }

            } else {
                this.rdioScannerService.beep(RdioScannerBeepStyle.Denied);
            }

            this.updateDimmer();
        }
    }

    holdSystem(): void {
        if (this.auth) {
            this.authFocus();

        } else {
            if (this.call || this.callPrevious) {
                this.rdioScannerService.beep(this.holdSys ? RdioScannerBeepStyle.Deactivate : RdioScannerBeepStyle.Activate);

                this.rdioScannerService.holdSystem();

            } else {
                this.rdioScannerService.beep(RdioScannerBeepStyle.Denied);
            }

            this.updateDimmer();
        }
    }

    holdTalkgroup(): void {
        if (this.auth) {
            this.authFocus();

        } else {
            if (this.call || this.callPrevious) {
                this.rdioScannerService.beep(this.holdTg ? RdioScannerBeepStyle.Deactivate : RdioScannerBeepStyle.Activate);

                this.rdioScannerService.holdTalkgroup();

            } else {
                this.rdioScannerService.beep(RdioScannerBeepStyle.Denied);
            }

            this.updateDimmer();
        }
    }

    livefeed(): void {
        if (this.auth) {
            this.authFocus();

        } else {
            this.rdioScannerService.beep(this.livefeedOffline ? RdioScannerBeepStyle.Activate : RdioScannerBeepStyle.Deactivate);

            this.rdioScannerService.livefeed();

            this.updateDimmer();
        }
    }

    ngOnDestroy(): void {
        this.clockTimer?.unsubscribe();

        this.eventSubscription.unsubscribe();
    }

    ngOnInit(): void {
        this.syncClock();
    }

    pause(): void {
        if (this.auth) {
            this.authFocus();

        } else {
            if (this.livefeedPaused) {
                this.rdioScannerService.beep(RdioScannerBeepStyle.Deactivate);

                this.rdioScannerService.pause();

            } else {
                this.rdioScannerService.beep(RdioScannerBeepStyle.Activate);

                this.rdioScannerService.pause();
            }

            this.updateDimmer();
        }
    }

    replay(): void {
        if (this.auth) {
            this.authFocus();

        } else {
            if (!this.livefeedPaused && (this.call || this.callPrevious)) {
                this.rdioScannerService.beep(RdioScannerBeepStyle.Activate);

                if (this.replayTimer instanceof Subscription) {
                    this.replayTimer.unsubscribe();
                    this.replayOffset = Math.min(this.callHistory.length, this.replayOffset + 1);
                }

                this.replayTimer = timer(1000).subscribe(() => {
                    this.replayTimer = undefined;
                    this.replayOffset = 0;
                });

                if (this.call && !this.replayOffset) {
                    this.rdioScannerService.replay()
                } else if (this.callPrevious !== this.callHistory[0]) {
                    if (this.replayOffset) {
                        this.rdioScannerService.play(this.callHistory[this.replayOffset - 1]);
                    } else {
                        this.rdioScannerService.replay()
                    }
                } else if (this.replayOffset < this.callHistory.length) {
                    this.rdioScannerService.play(this.callHistory[this.replayOffset]);
                }

            } else {
                this.rdioScannerService.beep(RdioScannerBeepStyle.Denied);
            }

            this.updateDimmer();
        }
    }

    showSearchPanel(): void {
        if (!this.config) {
            return;
        }

        if (this.auth) {
            this.authFocus();

        } else {
            this.rdioScannerService.beep();

            this.openSearchPanel.emit();
        }
    }

    showSelectPanel(): void {
        if (!this.config) {
            return;
        }

        if (this.auth) {
            this.authFocus();

        } else {
            this.rdioScannerService.beep();

            this.openSelectPanel.emit();
        }
    }

    skip(options?: { delay?: boolean }): void {
        if (this.auth) {
            this.authFocus();

        } else {
            this.rdioScannerService.beep(RdioScannerBeepStyle.Activate);

            this.rdioScannerService.skip(options);

            this.updateDimmer();
        }
    }

    stop(): void {
        this.rdioScannerService.stop();
    }

    private eventHandler(event: RdioScannerEvent): void {
        if ('auth' in event && event.auth) {
            let password: string | null = null;

            password = window?.localStorage?.getItem(LOCAL_STORAGE_KEY);

            if (password) {
                password = window.atob(password);

                window.localStorage.removeItem(LOCAL_STORAGE_KEY);
            }

            if (password) {
                this.authForm.get('password')?.setValue(password);

                this.rdioScannerService.authenticate(password);

            } else {
                this.auth = event.auth;

                this.authForm.reset();

                if (this.authForm.disabled) {
                    this.authForm.enable();
                }
            }
        }

        if ('call' in event) {
            if (this.call) {
                this.callPrevious = this.call;

                this.call = undefined;
            }

            if (event.call) {
                this.call = event.call;

                this.updateDimmer();
            }
        }

        if ('config' in event) {
            this.config = event.config;

            const password = this.authForm.get('password')?.value;

            if (password) {
                window?.localStorage?.setItem(LOCAL_STORAGE_KEY, window.btoa(password));

                this.authForm.reset();
            }

            this.auth = false;

            this.authForm.reset();

            if (this.authForm.enabled) {
                this.authForm.disable();
            }
        }

        if ('expired' in event && event.expired === true) {
            this.authForm.get('password')?.setErrors({ expired: true });
        }

        if ('holdSys' in event) {
            this.holdSys = event.holdSys || false;
        }

        if ('holdTg' in event) {
            this.holdTg = event.holdTg || false;
        }

        if ('linked' in event) {
            this.linked = event.linked || false;
        }

        if ('listeners' in event) {
            this.listeners = event.listeners || 0;
        }

        if ('map' in event) {
            this.map = event.map || {};
        }

        if ('pause' in event) {
            this.livefeedPaused = event.pause || false;
        }

        if ('queue' in event) {
            this.callQueue = event.queue || 0;
        }

        if ('time' in event && typeof event.time === 'number') {
            this.callTime = event.time;

            this.updateDimmer();
        }

        if ('tooMany' in event && event.tooMany === true) {
            this.authForm.get('password')?.setErrors({ tooMany: true });
        }

        if ('livefeedMode' in event && event.livefeedMode) {
            this.livefeedOffline = event.livefeedMode === RdioScannerLivefeedMode.Offline;

            this.livefeedOnline = event.livefeedMode === RdioScannerLivefeedMode.Online;

            this.playbackMode = event.livefeedMode === RdioScannerLivefeedMode.Playback;

            return;
        }

        this.updateDisplay();
    }

    private formatAfs(n: number): string {
        return `${(n >> 7 & 15).toString().padStart(2, '0')}-${(n >> 3 & 15).toString().padStart(2, '0')}${n & 7}`;
    }

    private formatFrequency(frequency: number | undefined): string {
        return typeof frequency === 'number' ? frequency
            .toString()
            .padStart(9, '0')
            .replace(/(\d)(?=(\d{3})+$)/g, '$1 ')
            .concat(' Hz') : '';
    }

    private isAfsSystem(talkgroupId: number): boolean {
        if (typeof this.config?.afs !== 'string') {
            return false;
        }

        return this.config.afs.split(',').includes(talkgroupId.toString());
    }

    private syncClock(): void {
        this.clockTimer?.unsubscribe();

        this.clock = new Date();

        this.clockTimer = timer(1000 * (60 - this.clock.getSeconds())).subscribe(() => this.syncClock());
    }

    private updateDimmer(): void {
        if (typeof this.config?.dimmerDelay === 'number') {
            this.dimmerTimer?.unsubscribe();

            this.dimmer = true;

            this.dimmerTimer = timer(this.config.dimmerDelay).subscribe(() => {
                this.dimmerTimer?.unsubscribe();

                this.dimmerTimer = undefined;

                this.dimmer = false;

                this.ngChangeDetectorRef.detectChanges();
            });
        }
    }

    private updateDisplay(time = this.callTime): void {
        if (this.call) {
            let isAfs = this.isAfsSystem(this.call.system);

            this.callProgress = new Date(this.call.dateTime);
            this.callProgress.setSeconds(this.callProgress.getSeconds() + time);

            this.callSystem = this.call.systemData?.label || `${this.call.system}`;

            this.callTag = this.call.talkgroupData?.tag || '';

            this.callTalkgroup = this.call.talkgroupData?.label || `${isAfs ? this.formatAfs(this.call.talkgroup) : this.call.talkgroup}`;

            this.callTalkgroupName = this.call.talkgroupData?.name || this.formatFrequency(this.call?.frequency);

            if (Array.isArray(this.call.frequencies) && this.call.frequencies.length) {
                const frequency = this.call.frequencies.reduce((p, v) => (v.pos || 0) <= time ? v : p, {});

                this.callError = typeof frequency.errorCount === 'number' ? `${frequency.errorCount}` : '';

                this.callFrequency = this.formatFrequency(typeof frequency.freq === 'number' ? frequency.freq : this.call.frequency);

                this.callSpike = typeof frequency.spikeCount === 'number' ? `${frequency.spikeCount}` : '';

            } else {
                this.callError = '';

                this.callFrequency = typeof this.call.frequency === 'number'
                    ? this.formatFrequency(this.call.frequency)
                    : '';

                this.callSpike = '';
            }

            if (Array.isArray(this.call.sources) && this.call.sources.length) {
                const source = this.call.sources.reduce((p, v) => (v.pos || 0) <= time ? v : p, {});

                this.callTalkgroupId = isAfs ? this.formatAfs(this.call.talkgroup) : this.call.talkgroup.toString();

                if (typeof source.src === 'number' && Array.isArray(this.call.systemData?.units)) {
                    const callUnit = this.call?.systemData?.units?.find((u) => u.id === source.src);

                    this.callUnit = callUnit ? callUnit.label : `${source.src}`;

                } else {
                    this.callUnit = typeof this.call.source === 'number' ? `${this.call.source}` : '';
                }

            } else {
                this.callTalkgroupId = isAfs ? this.formatAfs(this.call.talkgroup) : this.call.talkgroup.toString();

                this.callUnit = typeof this.call.source === 'number' ? `${this.call.source}` : '';
            }

            if (
                this.callPrevious &&
                this.callPrevious.id !== this.call.id &&
                !this.callHistory.find((call: RdioScannerCall) => call?.id === this.callPrevious?.id)
            ) {
                this.callHistory.pop();

                this.callHistory.unshift(this.callPrevious);
            }
        }

        const call = this.call || this.callPrevious;

        if (call) {
            if (this.rdioScannerService.isPatched(call)) {
                this.avoided = false;
                this.patched = true;
            } else {
                this.avoided = this.rdioScannerService.isAvoided(call);
                this.patched = false;
            }
        }

        const colors = ['blue', 'cyan', 'green', 'magenta', 'orange', 'red', 'white', 'yellow'];

        this.ledStyle = this.call && this.livefeedPaused ? 'on paused' : this.call ? 'on' : 'off';

        if (colors.includes(this.call?.talkgroupData?.led as string)) {
            this.ledStyle = `${this.ledStyle} ${this.call?.talkgroupData?.led}`;

        } else if (colors.includes(this.call?.systemData?.led as string)) {
            this.ledStyle = `${this.ledStyle} ${this.call?.systemData?.led}`;
        }

        this.ngChangeDetectorRef.detectChanges();
    }
}
