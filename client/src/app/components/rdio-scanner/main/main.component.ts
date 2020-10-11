/*
 * *****************************************************************************
 * Copyright (C) 2019-2020 Chrystian Huot
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
import { name as appName, version } from '../../../../../package.json';
import { RdioScannerAvoidOptions, RdioScannerBeepStyle, RdioScannerCall, RdioScannerConfig, RdioScannerEvent, RdioScannerLivefeedMode } from '../rdio-scanner';
import { AppRdioScannerService } from '../rdio-scanner.service';

const LOCAL_STORAGE_KEY = AppRdioScannerService.LOCAL_STORAGE_KEY + '-pin';

@Component({
    selector: 'app-rdio-scanner-main',
    styleUrls: [
        '../common.scss',
        './main.component.scss',
    ],
    templateUrl: './main.component.html',
})
export class AppRdioScannerMainComponent implements OnDestroy, OnInit {
    auth = false;
    authForm = this.ngFormBuilder.group({ password: [] });

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
    callTalkgroupName = `Rdio Scanner ${appName === 'rdio-scanner-client' ? 'v'.concat(version) : ''}`;
    callTime = 0;
    callUnit = '0';

    clock = new Date();

    dimmer = false;

    holdSys = false;
    holdTg = false;

    ledStyle = '';

    livefeedOffline = true;
    livefeedOnline = false;
    livefeedPaused = false;

    playbackMode = false;

    @Output() openSearchPanel = new EventEmitter<void>();

    @Output() openSelectPanel = new EventEmitter<void>();

    @Output() toggleFullscreen = new EventEmitter<void>();

    @ViewChild('password', { read: MatInput }) private authPassword: MatInput | undefined;

    private clockTimer: Subscription | undefined;

    private config: RdioScannerConfig | undefined;

    private dimmerTimer: Subscription | undefined;

    private eventSubscription = this.appRdioScannerService.event.subscribe((event: RdioScannerEvent) => this.eventHandler(event));

    constructor(
        private appRdioScannerService: AppRdioScannerService,
        private ngChangeDetectorRef: ChangeDetectorRef,
        private ngFormBuilder: FormBuilder,
    ) { }

    authenticate(password = this.authForm.value.password): void {
        this.authForm.disable();

        this.appRdioScannerService.authenticate(password);
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
            if (options || this.call || this.callPrevious) {
                this.appRdioScannerService.beep(RdioScannerBeepStyle.Activate);

                this.appRdioScannerService.avoid(options);

            } else {
                this.appRdioScannerService.beep(RdioScannerBeepStyle.Denied);
            }

            this.updateDimmer();
        }
    }

    holdSystem(): void {
        if (this.auth) {
            this.authFocus();

        } else {
            if (this.call || this.callPrevious) {
                this.appRdioScannerService.beep(this.holdSys ? RdioScannerBeepStyle.Deactivate : RdioScannerBeepStyle.Activate);

                this.appRdioScannerService.holdSystem();

            } else {
                this.appRdioScannerService.beep(RdioScannerBeepStyle.Denied);
            }

            this.updateDimmer();
        }
    }

    holdTalkgroup(): void {
        if (this.auth) {
            this.authFocus();

        } else {
            if (this.call || this.callPrevious) {
                this.appRdioScannerService.beep(this.holdTg ? RdioScannerBeepStyle.Deactivate : RdioScannerBeepStyle.Activate);

                this.appRdioScannerService.holdTalkgroup();

            } else {
                this.appRdioScannerService.beep(RdioScannerBeepStyle.Denied);
            }

            this.updateDimmer();
        }
    }

    livefeed(): void {
        if (this.auth) {
            this.authFocus();

        } else {
            this.appRdioScannerService.beep(this.livefeedOffline ? RdioScannerBeepStyle.Activate : RdioScannerBeepStyle.Deactivate);

            this.appRdioScannerService.livefeed();

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
                this.appRdioScannerService.beep(RdioScannerBeepStyle.Deactivate).then(() => this.appRdioScannerService.pause());

            } else {
                this.appRdioScannerService.beep(RdioScannerBeepStyle.Activate);

                this.appRdioScannerService.pause();
            }

            this.updateDimmer();
        }
    }

    replay(): void {
        if (this.auth) {
            this.authFocus();

        } else {
            if (!this.livefeedPaused && (this.call || this.callPrevious)) {
                this.appRdioScannerService.beep(RdioScannerBeepStyle.Activate);

                this.appRdioScannerService.replay();

            } else {
                this.appRdioScannerService.beep(RdioScannerBeepStyle.Denied);
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
            this.appRdioScannerService.beep();

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
            this.appRdioScannerService.beep();

            this.openSelectPanel.emit();
        }
    }

    skip(options?: { delay?: boolean }): void {
        if (this.auth) {
            this.authFocus();

        } else {
            this.appRdioScannerService.beep(RdioScannerBeepStyle.Activate);

            this.appRdioScannerService.skip(options);

            this.updateDimmer();
        }
    }

    stop(): void {
        this.appRdioScannerService.stop();
    }

    private eventHandler(event: RdioScannerEvent): void {
        if ('auth' in event && event.auth) {
            let password: string | null = null;

            password = window?.localStorage?.getItem(LOCAL_STORAGE_KEY);

            if (password) {
                password = atob(password);

                window.localStorage.removeItem(LOCAL_STORAGE_KEY);
            }

            if (password) {
                this.authForm.get('password')?.setValue(password);

                this.appRdioScannerService.authenticate(password);

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
                window?.localStorage?.setItem(LOCAL_STORAGE_KEY, btoa(password));

                this.authForm.reset();
            }

            this.auth = false;

            this.authForm.reset();

            if (this.authForm.enabled) {
                this.authForm.disable();
            }
        }

        if ('holdSys' in event) {
            this.holdSys = event.holdSys || false;
        }

        if ('holdTg' in event) {
            this.holdTg = event.holdTg || false;
        }

        if ('livefeedMode' in event && event.livefeedMode) {
            this.livefeedOffline = event.livefeedMode === RdioScannerLivefeedMode.Offline;

            this.livefeedOnline = event.livefeedMode === RdioScannerLivefeedMode.Online;

            this.playbackMode = event.livefeedMode === RdioScannerLivefeedMode.Playback;
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

        this.updateDisplay();
    }

    private formatFrequency(frequency: number | undefined): string {
        return typeof frequency === 'number' ? frequency
            .toString()
            .padStart(9, '0')
            .replace(/(\d)(?=(\d{3})+$)/g, '$1 ')
            .concat(' Hz') : '';
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
            this.callProgress = new Date(this.call.dateTime);
            this.callProgress.setSeconds(this.callProgress.getSeconds() + time);

            this.callSystem = this.call.systemData?.label || `${this.call.system}`;

            this.callTag = this.call.talkgroupData?.tag || '';

            this.callTalkgroup = this.call.talkgroupData?.label || `${this.call.talkgroup}`;

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
                    : this.call.audioName?.replace(/\..+$/, '') || '';

                this.callSpike = '';
            }

            if (Array.isArray(this.call.sources) && this.call.sources.length) {
                const source = this.call.sources.reduce((p, v) => (v.pos || 0) <= time ? v : p, {});

                this.callTalkgroupId = `${this.call.talkgroup}`;

                if (typeof source.src === 'number' && Array.isArray(this.call.systemData?.units)) {
                    const callUnit = this.call?.systemData?.units?.find((u) => u.id === source.src);

                    this.callUnit = callUnit ? callUnit.label : `${source.src}`;

                } else {
                    this.callUnit = typeof this.call.source === 'number' ? `${this.call.source}` : '';
                }

            } else {
                this.callTalkgroupId = this.call.talkgroup.toString();

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

        const colors = ['blue', 'cyan', 'green', 'magenta', 'red', 'white', 'yellow'];

        this.ledStyle = this.call && this.livefeedPaused ? 'on paused' : this.call ? 'on' : 'off';

        if (colors.includes(this.call?.talkgroupData?.led as string)) {
            this.ledStyle = `${this.ledStyle} ${this.call?.talkgroupData?.led}`;

        } else if (colors.includes(this.call?.systemData?.led as string)) {
            this.ledStyle = `${this.ledStyle} ${this.call?.systemData?.led}`;
        }

        this.ngChangeDetectorRef.detectChanges();
    }
}
