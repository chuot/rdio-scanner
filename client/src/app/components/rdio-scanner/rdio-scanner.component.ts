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

import { DOCUMENT } from '@angular/common';
import { AfterViewInit, ChangeDetectorRef, Component, ElementRef, EventEmitter, HostListener, Inject, OnDestroy, Output, ViewChild } from '@angular/core';
import { FormBuilder, FormGroup } from '@angular/forms';
import { MatInput } from '@angular/material/input';
import { MatPaginator } from '@angular/material/paginator';
import { BehaviorSubject, NEVER, Subscription } from 'rxjs';
import { name as appName, version } from '../../../../package.json';
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
    providers: [AppRdioScannerService],
    selector: 'app-rdio-scanner',
    styleUrls: ['./rdio-scanner.component.scss'],
    templateUrl: './rdio-scanner.component.html',
})
export class AppRdioScannerComponent implements AfterViewInit, OnDestroy {
    @Output() readonly live = new EventEmitter<boolean>();

    auth = false;
    authForm = this.ngFormBuilder.group({
        password: [],
    });

    call: RdioScannerCall | undefined;
    callError = '0';
    callFrequency: string = this.formatFrequency(0);
    callHistory: RdioScannerCall[] = new Array<RdioScannerCall>(5);
    callPending: string | undefined;
    callPrevious: RdioScannerCall | undefined;
    callProgress = new Date(0, 0, 0, 0, 0, 0);
    callQueue = 0;
    callSpike = '0';
    callSystem = 'System';
    callTag = 'Tag';
    callTalkgroup = 'Talkgroup';
    callTalkgroupId = '0';
    callTalkgroupName = `Rdio Scanner ${appName === 'rdio-scanner-client' ? 'v'.concat(version.replace(/^(\d+\.\d+).*$/, '$1')) : ''}`;
    callTime = 0;
    callUnit = '0';

    clock = new Date();

    dimmerDelay: number | undefined;

    config: RdioScannerConfig | undefined;

    groups: RdioScannerGroup[] = [];

    holdSys = false;
    holdTg = false;

    ledClass = '';

    list: RdioScannerList | undefined;

    liveFeedActive = false;
    liveFeedOffline = false;
    liveFeedPaused = false;

    map: RdioScannerLiveFeedMap = {};

    searchPanelOpened = false;

    searchResults = new BehaviorSubject(new Array<RdioScannerCall | null>(10));
    searchResultsPending = false;

    selectPanelOpened = false;

    searchForm: FormGroup = this.ngFormBuilder.group({
        date: [null],
        sort: [-1],
        system: [-1],
        talkgroup: [-1],
    });

    @ViewChild('password', { read: MatInput }) private authPassword: MatInput | undefined;

    private clockRefresh: number | undefined;

    @ViewChild(MatPaginator, { read: MatPaginator }) private searchFormPaginator: MatPaginator | undefined;

    private searchResultsLimit = 200;
    private searchResultsOffset = 0;

    private subscriptions: Subscription[] = [];

    private visibilityChangeListener = () => this.syncClock();

    constructor(
        private appRdioScannerService: AppRdioScannerService,
        @Inject(DOCUMENT) private document: Document,
        private ngChangeDetectorRef: ChangeDetectorRef,
        private ngElementRef: ElementRef,
        private ngFormBuilder: FormBuilder,
    ) { }

    authenticate(password = this.authForm.get('password')?.value): void {
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

        } else if (!this.liveFeedOffline) {
            this.appRdioScannerService.avoid(options);

            this.updateDimmer();
        }
    }

    download(id: string): void {
        this.appRdioScannerService.loadAndDownload(id);
    }

    @HostListener('window:beforeunload', ['$event'])
    exitNotification(event: BeforeUnloadEvent): void {
        if (this.liveFeedActive || this.liveFeedOffline) {
            event.preventDefault();

            event.returnValue = 'Live Feed is ON, do you really want to leave?';
        }
    }

    getSearchFormTalkgroups(): RdioScannerTalkgroup[] {
        const system = this.config?.systems?.find((sys) => sys.id === this.searchForm.get('system')?.value);

        return system ? system.talkgroups : [];
    }

    holdSystem(): void {
        if (this.auth) {
            this.authFocus();

        } else if (!this.liveFeedOffline) {
            this.appRdioScannerService.holdSystem();

            this.updateDimmer();
        }
    }

    holdTalkgroup(): void {
        if (this.auth) {
            this.authFocus();

        } else if (!this.liveFeedOffline) {
            this.appRdioScannerService.holdTalkgroup();

            this.updateDimmer();
        }
    }

    liveFeed(): void {
        if (this.auth) {
            this.authFocus();

        } else if (this.liveFeedOffline) {
            this.stopOfflinePlay();

        } else {
            this.appRdioScannerService.liveFeed();

            this.updateDimmer();
        }
    }

    ngAfterViewInit(): void {
        // Refresh display on visibility change

        this.document.addEventListener('visibilitychange', this.visibilityChangeListener);

        // initialize the realtime clock

        this.syncClock();

        // subscribe to events

        let searchFormPreviousValue = this.searchForm.value;

        this.subscriptions.push(

            // service events

            this.appRdioScannerService.event.subscribe((event: RdioScannerEvent) => {
                if ('auth' in event) {
                    if (event.auth) {
                        let password: string | null = null;

                        if (window instanceof Window && window.localStorage instanceof Storage) {
                            password = window.localStorage.getItem(LOCAL_STORAGE_KEY);

                            if (password) {
                                password = atob(password);

                                window.localStorage.removeItem(LOCAL_STORAGE_KEY);
                            }
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

                            setTimeout(() => this.authFocus());
                        }
                    }
                }

                if ('call' in event) {
                    if (this.call) {
                        this.callPrevious = this.call;

                        this.call = undefined;
                    }

                    if (event.call) {
                        this.call = this.transformCall(event.call);

                        if (this.callPending === this.call.id) {
                            this.callPending = undefined;
                        }

                        if (this.liveFeedOffline) {
                            this.setOfflineQueueCount(this.call.id);
                        }

                        this.updateDimmer();

                    } else if (this.liveFeedOffline) {
                        this.playNextCall();
                    }
                }

                if ('config' in event) {
                    this.config = event.config;

                    const password = this.authForm.get('password')?.value;

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

                    this.searchResultsPending = false;

                    if (this.searchPanelOpened) {
                        this.searchCall();

                    } else if (this.searchForm.disabled) {
                        this.searchForm.enable();
                    }
                }

                if (Array.isArray(event?.groups)) {
                    this.groups = event.groups;
                }

                if (typeof event?.holdSys === 'boolean') {
                    this.holdSys = event.holdSys;
                }

                if (typeof event?.holdTg === 'boolean') {
                    this.holdTg = event.holdTg;
                }

                if (typeof event?.liveFeed === 'boolean') {
                    this.liveFeedActive = event.liveFeed;

                    this.live.emit(this.liveFeedActive);
                }

                if (typeof event?.list === 'object') {
                    this.list = event.list;

                    if (Array.isArray(this.list.results)) {
                        this.list.results = this.list.results.map((call) => this.transformCall(call));
                    }

                    this.refreshSearchResults();

                    this.searchResultsPending = false;

                    this.searchForm.enable();

                    if (!this.call && this.liveFeedOffline) {
                        if (this.searchForm.get('sort')?.value === -1) {
                            const nextCall = this.list.results[this.searchResultsLimit - 1];

                            if (nextCall) {
                                this.appRdioScannerService.loadAndPlay(nextCall.id);
                            }

                        } else {
                            const nextCall = this.list.results[0];

                            if (nextCall) {
                                this.appRdioScannerService.loadAndPlay(nextCall.id);
                            }
                        }
                    }
                }

                if (typeof event?.map === 'object') {
                    this.map = event.map;
                }

                if (typeof event?.pause === 'boolean') {
                    this.liveFeedPaused = event.pause;
                }

                if (typeof event?.queue === 'number') {
                    if (!this.liveFeedOffline) {
                        this.callQueue = event.queue;
                    }
                }

                if (typeof event?.time === 'number') {
                    this.callTime = event.time;

                    this.updateDimmer();
                }

                this.updateDisplay();
            }),

            // search form events

            this.searchForm.valueChanges.subscribe((value) => {
                if (!Object.keys(value).every((key) => value[key] === searchFormPreviousValue[key])) {
                    if (this.liveFeedOffline) {
                        this.stopOfflinePlay();
                    }

                    if (value.system !== searchFormPreviousValue.system && value.talkgroup !== -1) {
                        searchFormPreviousValue.talkgroup = -1;

                        this.searchForm.get('talkgroup')?.reset(searchFormPreviousValue.talkgroup);
                    }

                    if (this.searchFormPaginator instanceof MatPaginator) {
                        this.searchFormPaginator.firstPage();
                    }

                    searchFormPreviousValue = value;

                    this.searchCall();
                }
            }),

            // search results paginator events

            this.searchFormPaginator?.page.subscribe(() => this.refreshSearchResults()) || NEVER.subscribe(),

        );
    }

    ngOnDestroy(): void {
        // terminate subscriptions

        while (this.subscriptions.length) {
            this.subscriptions.pop()?.unsubscribe();
        }

        // stop the realtime clock

        if (this.clockRefresh) {
            clearTimeout(this.clockRefresh);
        }

        // terminate our live event emitter

        this.live.complete();

        // remove visibility change listener
        this.document.removeEventListener('visibilitychange', this.visibilityChangeListener);
    }

    pause(): void {
        if (this.auth) {
            this.authFocus();

        } else {
            this.appRdioScannerService.pause();

            this.updateDimmer();
        }
    }

    play(id: string): void {
        if (!this.callPending) {
            if (!this.liveFeedActive && !this.liveFeedOffline) {
                this.liveFeedOffline = true;

                if (this.holdSys) {
                    this.appRdioScannerService.holdSystem();

                } else if (this.holdTg) {
                    this.appRdioScannerService.holdTalkgroup();
                }

                this.setOfflineQueueCount(id);
            }

            this.callPending = id;

            this.appRdioScannerService.loadAndPlay(id);
        }
    }

    replay(): void {
        if (this.auth) {
            this.authFocus();

        } else {
            this.appRdioScannerService.replay();

            this.updateDimmer();
        }
    }

    resetSearchForm(): void {
        if (this.liveFeedOffline) {
            this.stopOfflinePlay();
        }

        this.searchForm.reset({
            date: null,
            sort: -1,
            system: -1,
            talkgroup: -1,
        });

        if (this.searchFormPaginator instanceof MatPaginator) {
            this.searchFormPaginator.firstPage();
        }

        this.searchCall();
    }

    searchCall(): void {
        if (!this.config || !(this.searchFormPaginator instanceof MatPaginator)) {
            return;
        }

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

    showSearchPanel(): void {
        if (!this.config) {
            return;
        }

        if (this.auth) {
            this.authFocus();

        } else {
            this.searchPanelOpened = true;

            if (!this.liveFeedOffline) {
                this.searchCall();
            }
        }
    }

    showSelectPanel(): void {
        if (!this.config) {
            return;
        }

        if (this.auth) {
            this.authFocus();

        } else {
            this.selectPanelOpened = true;
        }
    }

    skip(options?: { delay?: boolean }): void {
        if (this.auth) {
            this.authFocus();

        } else if (!this.callPending) {
            this.appRdioScannerService.skip(options);

            this.updateDimmer();
        }
    }

    stop(): void {
        if (this.liveFeedOffline) {
            this.stopOfflinePlay();
        }

        this.appRdioScannerService.stop();
    }

    toggleFullscreen(): void {
        if (document.fullscreenElement) {
            const el: {
                exitFullscreen?: () => void;
                mozCancelFullScreen?: () => void;
                msExitFullscreen?: () => void;
                webkitExitFullscreen?: () => void;
            } = document;

            if (el.exitFullscreen) {
                el.exitFullscreen();
            } else if (el.mozCancelFullScreen) {
                el.mozCancelFullScreen();
            } else if (el.msExitFullscreen) {
                el.msExitFullscreen();
            } else if (el.webkitExitFullscreen) {
                el.webkitExitFullscreen();
            }

        } else {
            const el = this.ngElementRef.nativeElement;

            if (el.requestFullscreen) {
                el.requestFullscreen();
            } else if (el.mozRequestFullScreen) {
                el.mozRequestFullScreen();
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

    private formatFrequency(frequency: number | undefined): string {
        return typeof frequency === 'number' ? frequency
            .toString()
            .padStart(9, '0')
            .replace(/(\d)(?=(\d{3})+$)/g, '$1 ')
            .concat(' Hz') : '';
    }

    private ngDetectChanges(): void {
        if (!this.document.hidden) {
            this.ngChangeDetectorRef.detectChanges();
        }
    }

    private playNextCall(): void {
        const callPreviousId = this.callPrevious?.id;

        const loadAndPlay = (id: string) => {
            this.callPending = id;

            setTimeout(() => this.appRdioScannerService.loadAndPlay(id), 750);
        };

        if (callPreviousId && this.list && this.searchFormPaginator instanceof MatPaginator) {
            const index = this.list.results.findIndex((call) => call.id === callPreviousId);

            if (index !== -1) {
                const order = this.searchForm.get('sort')?.value;

                if (order === -1) {
                    if (index > 0) {
                        const nextId = this.list.results[index - 1]?.id;

                        if (nextId) {
                            if (this.searchResults.value.find((call) => call?.id === callPreviousId)) {
                                const pageIndex = (index - 1) % this.searchResults.value.length;

                                if (pageIndex === this.searchResults.value.length - 1 && this.searchFormPaginator.hasPreviousPage()) {
                                    this.searchFormPaginator.previousPage();
                                }
                            }

                            loadAndPlay(nextId);

                        } else {
                            this.stopOfflinePlay();
                        }

                    } else if (this.searchFormPaginator.hasPreviousPage()) {
                        this.searchFormPaginator.previousPage();

                        this.searchCall();

                    } else {
                        this.stopOfflinePlay();
                    }

                } else {
                    if (index < this.searchResultsLimit - 1) {
                        const nextId = this.list.results[index + 1]?.id;

                        if (nextId) {
                            if (this.searchResults.value.find((call) => call?.id === callPreviousId)) {
                                const pageIndex = (index + 1) % this.searchResults.value.length;

                                if (pageIndex === 0 && this.searchFormPaginator.hasNextPage()) {
                                    this.searchFormPaginator.nextPage();
                                }
                            }

                            loadAndPlay(nextId);

                        } else {
                            this.stopOfflinePlay();

                        }

                    } else if (this.searchFormPaginator.hasNextPage()) {
                        this.searchFormPaginator.nextPage();

                        this.searchCall();

                    } else {
                        this.stopOfflinePlay();
                    }
                }
            }
        }
    }

    private refreshSearchResults(): void {
        if (this.searchFormPaginator instanceof MatPaginator) {
            const limit = this.searchResultsLimit;
            const offset = this.searchResultsOffset;
            const from = this.searchFormPaginator.pageIndex * this.searchFormPaginator.pageSize;
            const to = this.searchFormPaginator.pageIndex * this.searchFormPaginator.pageSize + this.searchFormPaginator.pageSize - 1;

            if (from >= offset + limit || from < offset) {
                this.searchCall();

            } else if (this.list) {
                const calls: Array<RdioScannerCall | null> = this.list.results.slice(from % limit, to % limit + 1);

                while (calls.length < this.searchResults.value.length) {
                    calls.push(null);
                }

                this.searchResults.next(calls);
            }
        }
    }

    private setOfflineQueueCount(id = this.call?.id || this.callPrevious?.id): void {
        if (id) {
            const index = this.list?.results.findIndex((call) => call.id === id);

            if (typeof index === 'number' && index !== -1) {
                if (this.searchForm.get('sort')?.value === -1) {
                    this.callQueue = this.searchResultsOffset + index;

                } else if (this.list) {
                    this.callQueue = this.list.count - this.searchResultsOffset - index - 1;
                }
            }
        }
    }

    private stopOfflinePlay(): void {
        this.liveFeedOffline = false;

        this.callQueue = 0;

        if (this.call) {
            this.appRdioScannerService.stop();
        }
    }

    private syncClock(): void {
        this.clock = new Date();

        if (this.clockRefresh) {
            clearTimeout(this.clockRefresh);
        }

        this.clockRefresh = setTimeout(() => this.syncClock(), (60 - this.clock.getSeconds()) * 1000);

        this.ngDetectChanges();
    }

    private transformCall(call: RdioScannerCall): RdioScannerCall {
        if (call && Array.isArray(this.config && this.config.systems)) {
            call.systemData = this.config?.systems.find((sys) => sys.id === call.system);

            if (Array.isArray(call.systemData?.talkgroups)) {
                call.talkgroupData = call.systemData?.talkgroups.find((tg) => tg.id === call.talkgroup);
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

                this.ngDetectChanges();
            }, 5000);

            this.ngDetectChanges();
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

                this.callFrequency = typeof this.call.frequency === 'number' ? this.formatFrequency(this.call.frequency) : '';

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
                this.callTalkgroupId = '';

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

        this.ledClass = this.call && this.liveFeedPaused ? 'on paused' : this.call ? 'on' : 'off';

        if (colors.includes(this.call?.talkgroupData?.led as string)) {
            this.ledClass = `${this.ledClass} ${this.call?.talkgroupData?.led}`;

        } else if (colors.includes(this.call?.systemData?.led as string)) {
            this.ledClass = `${this.ledClass} ${this.call?.systemData?.led}`;
        }

        this.ngDetectChanges();
    }
}
