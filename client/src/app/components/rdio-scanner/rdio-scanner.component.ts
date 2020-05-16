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

import {
    ChangeDetectionStrategy,
    ChangeDetectorRef,
    Component,
    ElementRef,
    EventEmitter,
    HostListener,
    OnDestroy,
    OnInit,
    Output,
    ViewChild,
} from '@angular/core';
import { FormBuilder, FormGroup } from '@angular/forms';
import { MatInput } from '@angular/material/input';
import { MatPaginator } from '@angular/material/paginator';
import { BehaviorSubject, Subscription } from 'rxjs';
import { version } from '../../../../package.json';
import {
    RdioScannerAvoidOptions,
    RdioScannerCall,
    RdioScannerConfig,
    RdioScannerEvent,
    RdioScannerGroup,
    RdioScannerList,
    RdioScannerLiveFeedMap,
    RdioScannerSearchOptions,
    RdioScannerTalkgroup,
} from './rdio-scanner';
import { AppRdioScannerService } from './rdio-scanner.service';

const LOCAL_STORAGE_KEY = AppRdioScannerService.LOCAL_STORAGE_KEY + '-pin';

@Component({
    changeDetection: ChangeDetectionStrategy.OnPush,
    providers: [AppRdioScannerService],
    selector: 'app-rdio-scanner',
    styleUrls: ['./rdio-scanner.component.scss'],
    templateUrl: './rdio-scanner.component.html',
})
export class AppRdioScannerComponent implements OnDestroy, OnInit {
    @Output() readonly live = new EventEmitter<boolean>();

    auth = false;
    authForm = this.ngFormBuilder.group({
        password: [],
    });

    call: RdioScannerCall;
    callError = '0';
    callFrequency: string = this.formatFrequency(0);
    callHistory: RdioScannerCall[] = new Array<RdioScannerCall>(5);
    callPrevious: RdioScannerCall;
    callProgress = new Date(0, 0, 0, 0, 0, 0);
    callQueue = 0;
    callSpike = '0';
    callSystem = 'System';
    callTag = 'Tag';
    callTalkgroup = 'Talkgroup';
    callTalkgroupId = '0';
    callTalkgroupName = `Rdio Scanner v${version.replace(/^(\d+\.\d+).*$/, '$1')}`;
    callTime = 0;
    callUnit = '0';

    clock = new Date();

    dimmerDelay: any;

    config: RdioScannerConfig;

    groups: RdioScannerGroup[] = [];

    holdSys = false;
    holdTg = false;

    ledClass: string;

    list: RdioScannerList;

    liveFeedActive = false;
    liveFeedPaused = false;

    map: RdioScannerLiveFeedMap = {};

    searchPanelOpened = false;

    searchResults = new BehaviorSubject(new Array<RdioScannerCall>(10));
    searchResultsPending = false;

    selectPanelOpened = false;

    searchForm: FormGroup = this.ngFormBuilder.group({
        date: [null],
        sort: [-1],
        system: [-1],
        talkgroup: [-1],
    });

    @ViewChild('password', { read: MatInput, static: true }) private authPassword: MatInput;

    private clockInterval: any;

    @ViewChild(MatPaginator, { static: true }) private searchFormPaginator: MatPaginator;

    private searchResultsLimit = 200;
    private searchResultsOffset = 0;

    private subscriptions: Subscription[] = [];

    constructor(
        private appRdioScannerService: AppRdioScannerService,
        private ngChangeDetectorRef: ChangeDetectorRef,
        private ngElementRef: ElementRef,
        private ngFormBuilder: FormBuilder,
    ) { }

    authenticate(password = this.authForm.get('password').value): void {
        this.authForm.disable();

        this.appRdioScannerService.authenticate(password);
    }

    authFocus() {
        if (this.auth) {
            this.authPassword.focus();
        }
    }

    avoid(options?: RdioScannerAvoidOptions): void {
        if (this.auth) {
            this.authPassword.focus();

        } else {
            this.appRdioScannerService.avoid(options);

            this.updateDimmer();
        }
    }

    download(id: number): void {
        this.appRdioScannerService.loadAndDownload(id);
    }

    @HostListener('window:beforeunload', ['$event'])
    exitNotification(event: BeforeUnloadEvent): void {
        if (this.liveFeedActive) {
            event.preventDefault();

            event.returnValue = 'Live Feed is ON, do you really want to leave?';
        }
    }

    getSearchFormTalkgroups(): RdioScannerTalkgroup[] {
        const system = this.config?.systems?.find((sys) => sys.id === this.searchForm.get('system').value);

        return system ? system.talkgroups : [];
    }

    holdSystem(): void {
        if (this.auth) {
            this.authPassword.focus();

        } else {
            this.appRdioScannerService.holdSystem();

            this.updateDimmer();
        }
    }

    holdTalkgroup(): void {
        if (this.auth) {
            this.authPassword.focus();

        } else {
            this.appRdioScannerService.holdTalkgroup();

            this.updateDimmer();
        }
    }

    liveFeed(): void {
        if (this.auth) {
            this.authPassword.focus();

        } else {
            this.appRdioScannerService.liveFeed();

            this.updateDimmer();
        }
    }

    ngOnDestroy() {
        // terminate subscriptions

        while (this.subscriptions.length) {
            this.subscriptions.pop().unsubscribe();
        }

        // stop the realtime clock

        if (this.clockInterval) {
            clearInterval(this.clockInterval);
        }

        // terminate our live event emitter

        this.live.complete();
    }

    ngOnInit() {
        // initialize the realtime clock

        setTimeout(() => {
            this.clock = new Date();

            this.ngChangeDetectorRef.markForCheck();

            this.clockInterval = setInterval(() => {
                this.clock = new Date();

                this.ngChangeDetectorRef.detectChanges();
            }, 60 * 1000);
        }, (60 - new Date().getSeconds()) * 1000);

        // subscribe to events

        let searchFormPreviousValue = this.searchForm.value;

        this.subscriptions.push(

            // service events

            this.appRdioScannerService.event.subscribe((event: RdioScannerEvent) => {
                if ('auth' in event) {
                    if (event.auth) {
                        let password: string;

                        if (window instanceof Window && window.localStorage instanceof Storage) {
                            password = window.localStorage.getItem(LOCAL_STORAGE_KEY);

                            if (password) {
                                password = atob(password);

                                window.localStorage.removeItem(LOCAL_STORAGE_KEY);
                            }
                        }

                        if (password) {
                            this.authForm.get('password').setValue(password);

                            this.appRdioScannerService.authenticate(password);

                        } else {
                            this.auth = event.auth;

                            this.authForm.reset();

                            if (this.authForm.disabled) {
                                this.authForm.enable();
                            }

                            setTimeout(() => this.authPassword.focus());
                        }
                    }
                }

                if ('config' in event) {
                    this.config = event.config;

                    const password = this.authForm.get('password').value;

                    if (password) {
                        if (window instanceof Window && window.localStorage instanceof Storage) {
                            window.localStorage.setItem(LOCAL_STORAGE_KEY, btoa(password));
                        }

                        this.authForm.reset();
                    }

                    this.auth = false;

                    this.authForm.reset();

                    if (this.authForm.enabled) {
                        this.authForm.disable();
                    }

                    if (this.searchPanelOpened) {
                        this.searchResultsPending = false;

                        this.searchCall();
                    }
                }

                if ('call' in event) {
                    if (event.call) {
                        this.call = this.transformCall(event.call);

                        this.updateDimmer();

                    } else {
                        this.callPrevious = this.call;

                        this.call = null;
                    }
                }

                if ('groups' in event) {
                    this.groups = event.groups;
                }

                if ('holdSys' in event) {
                    this.holdSys = event.holdSys;
                }

                if ('holdTg' in event) {
                    this.holdTg = event.holdTg;
                }

                if ('liveFeed' in event) {
                    this.liveFeedActive = event.liveFeed;

                    this.live.emit(this.liveFeedActive);
                }

                if ('list' in event) {
                    this.list = event.list;

                    if (Array.isArray(this.list && this.list.results)) {
                        this.list.results = this.list.results.map((call) => this.transformCall(call));
                    }

                    this.refreshSearchResults();

                    this.searchResultsPending = false;

                    this.searchForm.enable();
                }

                if ('map' in event) {
                    this.map = event.map;
                }

                if ('pause' in event) {
                    this.liveFeedPaused = event.pause;
                }

                if ('queue' in event) {
                    this.callQueue = event.queue;
                }

                if ('time' in event) {
                    this.callTime = event.time;

                    this.updateDimmer();
                }

                this.updateDisplay();
            }),

            // search form events

            this.searchForm.valueChanges.subscribe((value) => {
                if (!Object.keys(value).every((key) => value[key] === searchFormPreviousValue[key])) {
                    if (value.system !== searchFormPreviousValue.system && value.talkgroup !== -1) {
                        searchFormPreviousValue.talkgroup = -1;

                        this.searchForm.get('talkgroup').reset(searchFormPreviousValue.talkgroup);
                    }

                    this.searchFormPaginator.firstPage();

                    searchFormPreviousValue = value;

                    this.searchCall();
                }
            }),

            // search results paginator events

            this.searchFormPaginator.page.subscribe(() => this.refreshSearchResults()),

        );
    }

    pause(): void {
        if (this.auth) {
            this.authPassword.focus();

        } else {
            this.appRdioScannerService.pause();

            this.updateDimmer();
        }
    }

    play(id: number): void {
        this.appRdioScannerService.loadAndPlay(id);
    }

    replay(): void {
        if (this.auth) {
            this.authPassword.focus();

        } else {
            this.appRdioScannerService.replay();

            this.updateDimmer();
        }
    }

    resetSearchForm(): void {
        this.searchForm.reset({
            date: null,
            sort: -1,
            system: -1,
            talkgroup: -1,
        });

        this.searchFormPaginator.firstPage();

        this.searchCall();
    }

    searchCall(): void {
        if (!this.config) {
            return;
        }

        if (this.auth) {
            this.authPassword.focus();

            return;
        }

        this.searchPanelOpened = true;

        if (!this.searchResultsPending) {
            const filter = this.searchForm.value;
            const limit = this.searchResultsLimit;
            const offset = this.searchFormPaginator.pageIndex * this.searchFormPaginator.pageSize;
            const options: RdioScannerSearchOptions = { limit };

            this.searchResultsOffset = Math.floor(offset / limit) * limit;

            if (filter.date instanceof Date) {
                options.date = filter.date;
            }

            if (this.searchResultsOffset) {
                options.offset = this.searchResultsOffset;
            }

            if (filter.sort < 0) {
                options.sort = -1;
            }

            if (filter.system >= 0) {
                options.system = filter.system;
            }

            if (filter.talkgroup >= 0) {
                options.talkgroup = filter.talkgroup;
            }

            this.searchResultsPending = true;

            this.searchForm.disable();

            this.appRdioScannerService.searchCalls(options);
        }
    }

    selectTg(): void {
        if (!this.config) {
            return;
        }

        if (this.auth) {
            this.authPassword.focus();

        } else {
            this.selectPanelOpened = true;
        }
    }

    skip(nodelay?: boolean): void {
        if (this.auth) {
            this.authPassword.focus();

        } else {
            this.appRdioScannerService.skip(nodelay);

            this.updateDimmer();
        }
    }

    toggleFullscreen(): void {
        if (document.fullscreenElement) {
            const el: any = document;

            if (el.exitFullscreen) {
                el.exitFullscreen();
            } else if (el.mozCancelFullScreen) {
                el.mozCancelFullScreen();
            } else if (el.msExitFullscreen) {
                el.msExitFullscreen();
            } else if (el.mozCancelFullScreen) {
                el.mozCancelFullScreen();
            } else if (el.webkitExitFullscreen) {
                el.webkitExitFullscreen();
            }

        } else {
            const el = this.ngElementRef.nativeElement;

            if (el.requestFullscreen) {
                el.requestFullscreen();
            } else if (el.mozRequestFullScreen) {
                el.mozRequestFullscreen();
            } else if (el.msRequestFullscreen) {
                el.msRequestFullscreen();
            } else if (el.webkitRequestFullscreen) {
                el.webkitRequestFullscreen();
            }
        }
    }

    toggleGroup(label: string): void {
        this.appRdioScannerService.toggleGroup(label);
    }

    private formatFrequency(frequency: number): string {
        return (typeof frequency === 'number' ? frequency : 0)
            .toString()
            .padStart(9, '0')
            .replace(/(\d)(?=(\d{3})+$)/g, '$1 ')
            .concat(' Hz');
    }

    private refreshSearchResults(): void {
        const limit = this.searchResultsLimit;
        const offset = this.searchResultsOffset;
        const from = this.searchFormPaginator.pageIndex * this.searchFormPaginator.pageSize;
        const to = this.searchFormPaginator.pageIndex * this.searchFormPaginator.pageSize + this.searchFormPaginator.pageSize - 1;

        if (from >= offset + limit || from < offset) {
            this.searchCall();

        } else {
            const calls = this.list.results.slice(from % limit, to % limit + 1);

            while (calls.length < this.searchResults.value.length) {
                calls.push(null);
            }

            this.searchResults.next(calls);
        }
    }

    private transformCall(call: RdioScannerCall): RdioScannerCall {
        if (call && Array.isArray(this.config && this.config.systems)) {
            call.systemData = this.config.systems.find((sys) => sys.id === call.system);

            if (Array.isArray(call.systemData.talkgroups)) {
                call.talkgroupData = call.systemData.talkgroups.find((tg) => tg.id === call.talkgroup);
            }

            if (call.talkgroupData?.frequency) {
                call.frequency = call.talkgroupData.frequency;
            }
        }

        return call;
    }

    private updateDimmer(): void {
        if (this.config?.useDimmer) {
            if (this.dimmerDelay) {
                clearTimeout(this.dimmerDelay);
            }

            this.dimmerDelay = setTimeout(() => {
                this.dimmerDelay = undefined;

                this.ngChangeDetectorRef.detectChanges();
            }, 4000);
        }

        this.ngChangeDetectorRef.detectChanges();
    }

    private updateDisplay(time = this.callTime): void {
        if (this.call) {
            this.callProgress = new Date(this.call.dateTime);
            this.callProgress.setSeconds(this.callProgress.getSeconds() + time);

            this.callSystem = this.call.systemData?.label || `${this.call.system}`;

            this.callTag = this.call.talkgroupData?.tag || '';

            this.callTalkgroup = this.call.talkgroupData?.label || `${this.call.talkgroup}`;

            this.callTalkgroupName = this.call.talkgroupData?.name || this.formatFrequency(this.call.frequency);

            if (Array.isArray(this.call.frequencies) && this.call.frequencies.length) {
                const frequency = this.call.frequencies.reduce((p, v) => v.pos <= time ? v : p, {});

                this.callError = typeof frequency.errorCount === 'number' ? `${frequency.errorCount}` : '';

                this.callFrequency = this.formatFrequency(typeof frequency.freq === 'number' ? frequency.freq : this.call.frequency);

                this.callSpike = typeof frequency.spikeCount === 'number' ? `${frequency.spikeCount}` : '';

            } else {
                this.callError = '';

                this.callFrequency = typeof this.call.frequency === 'number' ? this.formatFrequency(this.call.frequency) : '';

                this.callSpike = '';
            }

            if (Array.isArray(this.call.sources) && this.call.sources.length) {
                const source = this.call.sources.reduce((p, v) => v.pos <= time ? v : p, {});

                this.callTalkgroupId = `${this.call.talkgroup}`;

                if (typeof source.src === 'number' && Array.isArray(this.call.systemData?.units)) {
                    const callUnit = this.call.systemData.units?.find((u) => u.id === source.src);

                    this.callUnit = callUnit ? callUnit.label : `${source.src}`;

                } else {
                    this.callUnit = typeof this.call.source === 'number' ? `${this.call.source}` : '';
                }

            } else {
                this.callTalkgroupId = '';

                this.callUnit = typeof this.call.source === 'number' ? `${this.call.source}` : '';
            }

            if (
                this.call?.id !== this.callPrevious?.id &&
                !this.callHistory.find((call: RdioScannerCall) => call?.id === this.callPrevious?.id)
            ) {
                this.callHistory.pop();

                this.callHistory.unshift(this.callPrevious);
            }
        }

        const colors = ['blue', 'cyan', 'green', 'magenta', 'red', 'white', 'yellow'];

        this.ledClass = this.call && this.liveFeedPaused ? 'on paused' : this.call ? 'on' : 'off';

        if (colors.includes(this.call?.talkgroupData?.led)) {
            this.ledClass = `${this.ledClass} ${this.call.talkgroupData.led}`;

        } else if (colors.includes(this.call?.systemData?.led)) {
            this.ledClass = `${this.ledClass} ${this.call.systemData.led}`;
        }

        this.ngChangeDetectorRef.detectChanges();
    }
}
