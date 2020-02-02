import { Component, ElementRef, EventEmitter, HostListener, NgZone, OnDestroy, OnInit, Output, ViewChild } from '@angular/core';
import { FormBuilder, FormGroup } from '@angular/forms';
import { MatPaginator } from '@angular/material/paginator';
import { BehaviorSubject, Subscription } from 'rxjs';
import { AppRdioScannerCallQueryService } from './rdio-scanner-call-query.service';
import { AppRdioScannerCallSubscriptionService, RdioScannerCall } from './rdio-scanner-call-subscription.service';
import { AppRdioScannerCallsQueryService } from './rdio-scanner-calls-query.service';
import { AppRdioScannerConfigQueryService, RdioScannerConfig } from './rdio-scanner-config-query.service';
import { AppRdioScannerSystemsQueryService } from './rdio-scanner-systems-query.service';
import { AppRdioScannerSystemsSubscriptionService, RdioScannerSystem, RdioScannerTalkgroup } from './rdio-scanner-systems-subscription.service';

declare var webkitAudioContext: any;

const LOCAL_STORAGE_KEY = 'rdio-scanner';

enum RdioScannerGroupStatus {
    Off = 'off',
    On = 'on',
    Partial = 'partial',
}

interface RdioScannerGroup {
    name: string;
    status: RdioScannerGroupStatus;
}

interface RdioScannerSelection {
    [key: string]: {
        [key: string]: boolean;
    };
}

@Component({
    selector: 'app-rdio-scanner',
    styleUrls: ['./rdio-scanner.component.scss'],
    templateUrl: './rdio-scanner.component.html',
})
export class AppRdioScannerComponent implements OnDestroy, OnInit {
    @Output() readonly live = new EventEmitter<boolean>();

    get call() { return this._call; }
    get callHistory() { return this._callHistory; }
    get callPrevious() { return this._callPrevious; }
    get callQueue() { return this._callQueue; }
    get config() { return this._config; }
    get displayAlphaTag() { return this._displayAlphaTag; }
    get displayClock() { return this._displayClock; }
    get displayDescription() { return this._displayDescription; }
    get displayErrorCount() { return this._displayErrorCount; }
    get displayFrequency() { return this._displayFrequency; }
    get displayMode() { return this._displayMode; }
    get displayProgress() { return this._displayProgress; }
    get displaySpikeCount() { return this._displaySpikeCount; }
    get displaySystem() { return this._displaySystem; }
    get displayTag() { return this._displayTag; }
    get displayTalkgroup() { return this._displayTalkgroup; }
    get displayUnit() { return this._displayUnit; }
    get groups() { return this._groups; }
    get livefeedPaused() { return this._livefeedPaused; }
    get livefeedStatus() { return !!this.livefeedSubscription; }
    get searchForm() { return this._searchForm; }
    get searchFormDateStart() { return this._searchFormDateStart; }
    get searchFormDateStop() { return this._searchFormDateStop; }
    get searchPanelOpened() { return this._searchPanelOpened; }
    get searchResults() { return this._searchResults; }
    get searchResultsCount() { return this._searchResultsCount; }
    get searchResultsPending() { return this._searchResultsPending; }
    get selection() { return this._selection; }
    get selectionSystemHold() { return this._selectionSystemHold; }
    get selectionTalkgroupHold() { return this._selectionTalkgroupHold; }
    get selectPanelOpened() { return this._selectPanelOpened; }
    get systems() { return this._systems; }

    private _call: RdioScannerCall | null = null;
    private _callHistory: RdioScannerCall[] = new Array<RdioScannerCall>(5);
    private _callPrevious: RdioScannerCall | null = null;
    private _callQueue: RdioScannerCall[] = [];
    private _config: RdioScannerConfig = {};
    private _displayAlphaTag = 'Alpha tag';
    private _displayClock = new Date();
    private _displayDescription = 'Idle';
    private _displayErrorCount = 0;
    private _displayFrequency = this.formatFrequency(0);
    private _displayMode = 'D';
    private _displayProgress = new Date(0, 0, 0, 0, 0, 0);
    private _displaySpikeCount = 0;
    private _displaySystem = 'System';
    private _displayTag = 'Tag';
    private _displayTalkgroup = 0;
    private _displayUnit: number | string = 0;
    private _groups: RdioScannerGroup[] = [];
    private _livefeedPaused = false;
    private _searchForm: FormGroup = this.ngFormBuilder.group({
        date: [null],
        sort: [-1],
        system: [-1],
        talkgroup: [-1],
    });
    private _searchFormDateStart = '';
    private _searchFormDateStop = '';
    private _searchPanelOpened = false;
    private _searchResults = new BehaviorSubject(new Array<RdioScannerCall>(10));
    private _searchResultsCount = 0;
    private _searchResultsPending = false;
    private _selection: RdioScannerSelection = {};
    private _selectionSystemHold: RdioScannerSelection | null = null;
    private _selectionTalkgroupHold: RdioScannerSelection | null = null;
    private _selectPanelOpened = false;
    private _systems: RdioScannerSystem[] = [];

    private audioContext: AudioContext | null = null;
    private audioSource: AudioBufferSourceNode | null = null;
    private audioStartTime = NaN;
    private clockInterval: any = null;
    private livefeedSubscription: Subscription;
    private searchFormSubscription: Subscription;
    private searchPaginatorSubscription: Subscription;
    private searchResultsBuffer = new Array<RdioScannerCall>(200);
    private searchResultsBufferFrom = 0;
    private searchResultsBufferTo = this.searchResultsBuffer.length - 1;
    private systemsSubscription: Subscription;

    @ViewChild('downloadLink', { static: true }) private downloadLink: ElementRef;

    @ViewChild(MatPaginator, { static: true }) private matPaginator: MatPaginator;

    constructor(
        private appRdioScannerCallQuery: AppRdioScannerCallQueryService,
        private appRdioScannerCallsQuery: AppRdioScannerCallsQueryService,
        private appRdioScannerCallSubscription: AppRdioScannerCallSubscriptionService,
        private appRdioScannerConfigQuery: AppRdioScannerConfigQueryService,
        private appRdioScannerSystemsQuery: AppRdioScannerSystemsQueryService,
        private appRdioScannerSystemsSubscription: AppRdioScannerSystemsSubscriptionService,
        private ngElementRef: ElementRef,
        private ngFormBuilder: FormBuilder,
        private ngZone: NgZone,
    ) { }

    avoid(options: {
        all?: boolean;
        call?: RdioScannerCall;
        system?: RdioScannerSystem;
        talkgroup?: RdioScannerTalkgroup;
        status?: boolean
    } = {}): void {
        if (this.selectionSystemHold) {
            this._selectionSystemHold = null;
        }

        if (this.selectionTalkgroupHold) {
            this._selectionTalkgroupHold = null;
        }

        if (typeof options.all === 'boolean') {
            Object.keys(this.selection).map((sys: string) => +sys).forEach((sys: number) => {
                Object.keys(this.selection[sys]).map((tg: string) => +tg).forEach((tg: number) => {
                    this._selection[sys][tg] = typeof options.status === 'boolean' ? options.status : options.all;
                });
            });

        } else if (options.call) {
            const sys = options.call.system;
            const tg = options.call.talkgroup;
            this._selection[sys][tg] = typeof options.status === 'boolean' ? options.status : !this.selection[sys][tg];

        } else if (options.system && options.talkgroup) {
            const sys = options.system.system;
            const tg = options.talkgroup.dec;
            this._selection[sys][tg] = typeof options.status === 'boolean' ? options.status : !this.selection[sys][tg];

        } else if (options.system && !options.talkgroup) {
            const sys = options.system.system;
            Object.keys(this.selection[sys]).map((tg: string) => +tg).forEach((tg: number) => {
                this._selection[sys][tg] = typeof options.status === 'boolean' ? options.status : !this.selection[sys][tg];
            });

        } else {
            const call = this.call || this.callPrevious;

            if (call) {
                const sys = call.system;
                const tg = call.talkgroup;
                this._selection[sys][tg] = typeof options.status === 'boolean' ? options.status : !this.selection[sys][tg];
            }
        }

        this.buildGroups();

        if (this.call && !(this.selection && this.selection[this.call.system] &&
            this._selection[this.call.system][this.call.talkgroup])) {
            this.skip();
        }

        if (this.livefeedStatus) {
            this.unsubscribeLivefeed();
            this.subscribeLivefeed();
        }

        this.writeSelection();

        this.cleanQueue();
    }

    @HostListener('window:beforeunload', ['$event'])
    exitNotification(event: BeforeUnloadEvent): void {
        if (this.livefeedStatus) {
            event.preventDefault();
            event.returnValue = 'Live feed is ON, do you really want to leave this page?';
        }
    }

    getSearchFormTalkgroups(): RdioScannerTalkgroup[] {
        const system = this.systems.find((sys) => sys.system === this.searchForm.get('system').value);

        return system ? system.talkgroups : [];
    }

    holdSystem(resubscribe = true): void {
        const call = this.call || this.callPrevious;

        if (call && this.selection) {
            if (this.selectionSystemHold) {
                this._selection = this._selectionSystemHold;
                this._selectionSystemHold = null;

            } else {
                if (this.selectionTalkgroupHold) {
                    this.holdTalkgroup(false);
                }

                this._selectionSystemHold = this.selection;

                this._selection = Object.keys(this.selection).map((sys) => +sys).reduce((sa, sv) => {
                    const allOn = Object.keys(this.selection[sv]).every((key) => !this.selection[sv][key]);

                    sa[sv] = Object.keys(this.selection[sv]).map((tg) => +tg).reduce((ta, tv) => {
                        if (sv === call.system) {
                            ta[tv] = allOn || this.selection[sv][tv];
                        } else {
                            ta[tv] = false;
                        }
                        return ta;
                    }, {});
                    return sa;
                }, {});

                this.cleanQueue();
            }

            if (resubscribe && this.livefeedStatus) {
                this.unsubscribeLivefeed();
                this.subscribeLivefeed();
            }

            this.buildGroups();
        }
    }

    holdTalkgroup(resubscribe = true): void {
        const call = this.call || this.callPrevious;

        if (call && this.selection) {
            if (this.selectionTalkgroupHold) {
                this._selection = this._selectionTalkgroupHold;
                this._selectionTalkgroupHold = null;

            } else {
                if (this.selectionSystemHold) {
                    this.holdSystem(false);
                }

                this._selectionTalkgroupHold = this.selection;

                this._selection = Object.keys(this.selection).map((sys) => +sys).reduce((sa, sv) => {
                    sa[sv] = Object.keys(this.selection[sv]).map((tg) => +tg).reduce((ta, tv) => {
                        if (sv === call.system) {
                            ta[tv] = tv === call.talkgroup;
                        } else {
                            ta[tv] = false;
                        }
                        return ta;
                    }, {});
                    return sa;
                }, {});

                this.cleanQueue();
            }

            if (resubscribe && this.livefeedStatus) {
                this.unsubscribeLivefeed();
                this.subscribeLivefeed();
            }

            this.buildGroups();
        }
    }

    livefeed(status = !this.livefeedStatus): void {
        if (status) {
            this.subscribeLivefeed();

        } else {
            this.unsubscribeLivefeed();

            this._callQueue.splice(0, this.callQueue.length);

            this.stop();
        }

        this.live.emit(status);
    }

    loadAndDownload(call: RdioScannerCall): void {
        this.loadCall(call).then((_call) => this.download(_call));
    }

    loadAndPlay(call: RdioScannerCall): void {
        this.loadCall(call).then((_call) => this.play(_call));
    }

    ngOnDestroy(): void {
        this.live.complete();

        this.unsubscribeLivefeed();

        this.unsubscribeSearchForm();

        this.unsubscribeSearchPaginator();

        this.unsubscribeSystems();

        clearInterval(this.clockInterval);
    }

    ngOnInit(): void {
        this.clockInterval = setInterval(() => this._displayClock = new Date(), 1000);

        this.bootstrapAudio();

        this.getConfig().then((config) => {
            this._config = config;

            this.subscribeSystems().then(() => {
                this.subscribeSearchForm();
                this.subscribeSearchPaginator();
            });
        });

    }

    pause(status = !this.livefeedPaused): void {
        this._livefeedPaused = status;

        if (this.audioContext) {
            if (this.livefeedPaused) {
                this.audioContext.suspend();
            } else {
                this.audioContext.resume();

                this.play();
            }
        }
    }

    play(call?: RdioScannerCall): void {
        if (this.audioContext && !this.livefeedPaused) {
            if (!call && !this.call && this.callQueue.length) {
                call = this.callQueue.shift();
            }

            if (call && call.audio) {
                this.stop();

                if (this.callPrevious && this.callPrevious !== call && !this.callHistory.find((c) => c === this.callPrevious)) {
                    this._callHistory.pop();
                    this._callHistory.unshift(this.callPrevious);
                }

                this._call = call;

                const arrayBuffer = new ArrayBuffer(call.audio.data.length);
                const arrayBufferView = new Uint8Array(arrayBuffer);

                for (let i = 0; i < call.audio.data.length; i++) {
                    arrayBufferView[i] = call.audio.data[i];
                }

                this.updateDisplay();

                this.ngZone.run(() => this.audioContext.decodeAudioData(arrayBuffer, (buffer) => {
                    this.audioContext.resume().then(() => {
                        const displayInterval = setInterval(() => this.updateDisplay(), 500);

                        this.audioSource = this.audioContext.createBufferSource();

                        this.audioSource.buffer = buffer;
                        this.audioSource.connect(this.audioContext.destination);
                        this.audioSource.onended = () => this.ngZone.runTask(() => {
                            clearInterval(displayInterval);
                            this.skip();
                        });
                        this.audioSource.start();
                    });
                }, () => this.skip()));
            }
        }
    }

    queue(call: RdioScannerCall): void {
        const selection = this.selection;
        const sys = call.system;
        const tg = call.talkgroup;

        if (selection && selection[sys] && selection[sys][tg]) {
            this.callQueue.push(call);

            this.play();
        }
    }

    replay(): void {
        if (!this.livefeedPaused) {
            this.play(this.call || this.callPrevious);
        }
    }

    resetSearchForm(): void {
        this.searchForm.reset({
            date: null,
            sort: -1,
            system: -1,
            talkgroup: -1,
        });

        if (this.matPaginator.pageIndex > 0) {
            this.matPaginator.firstPage();
        }
    }

    skip(nodelay = false): void {
        this.stop();

        setTimeout(() => this.play(), nodelay ? 0 : 1000);
    }

    stop(): void {
        if (this.audioSource && this.call) {
            this.audioSource.onended = null;
            this.audioSource.stop();
            this.audioSource.disconnect();
            this.audioSource = null;

            this.audioStartTime = NaN;

            this._callPrevious = this._call;
            this._call = null;
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

    toggleGroup(name: string): void {
        const group = this.groups.find((_group) => _group.name === name);

        if (this.selectionSystemHold) {
            this._selectionSystemHold = null;
        }

        if (this.selectionTalkgroupHold) {
            this._selectionTalkgroupHold = null;
        }

        if (group) {
            const status = group.status === RdioScannerGroupStatus.On ? false : true;

            this.systems.forEach((system) => {
                system.talkgroups.forEach((talkgroup) => {
                    if (talkgroup.group === name) {
                        this.selection[system.system][talkgroup.dec] = status;
                    }
                });
            });

            this.buildGroups();

            if (this.call && !(this.selection && this.selection[this.call.system] &&
                this._selection[this.call.system][this.call.talkgroup])) {
                this.skip();
            }

            if (this.livefeedStatus) {
                this.unsubscribeLivefeed();
                this.subscribeLivefeed();
            }

            this.writeSelection();

            this.cleanQueue();
        }
    }

    toggleSearchPanel(opened = !this.searchPanelOpened): void {
        if (opened) {
            this.loadStoredCalls();
        }

        this._searchPanelOpened = opened;
    }

    toggleSelectPanel(opened = !this.selectPanelOpened): void {
        this._selectPanelOpened = opened;
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
                if (this.audioContext.state === 'suspended') {
                    this.audioContext.resume();
                }

                this.audioContext.resume().then(() => {
                    events.forEach((event) => document.body.removeEventListener(event, bootstrap));
                });
            }
        };

        events.forEach((event) => document.body.addEventListener(event, bootstrap));
    }

    private buildGroups(): void {
        if (!this.config.useGroup) {
            return;
        }

        const groups: RdioScannerGroup[] = [];

        this.systems.forEach((system: RdioScannerSystem) => {
            system.talkgroups
                .forEach((talkgroup: RdioScannerTalkgroup) => {
                    const group = groups.find((_group) => _group.name === talkgroup.group);

                    if (!group) {
                        const allOn = this.systems.every((sys) => {
                            return sys.talkgroups.filter((tg) => tg.group === talkgroup.group)
                                .every((tg) => this.selection[sys.system][tg.dec]);
                        });

                        const allOff = this.systems.every((sys) => {
                            return sys.talkgroups.filter((tg) => tg.group === talkgroup.group)
                                .every((tg) => !this.selection[sys.system][tg.dec]);
                        });

                        const name = talkgroup.group;

                        const status = allOn ? RdioScannerGroupStatus.On
                            : allOff ? RdioScannerGroupStatus.Off
                                : RdioScannerGroupStatus.Partial;

                        groups.push({ name, status });
                    }
                });
        });

        groups.sort((a, b) => a.name.localeCompare(b.name));

        this.groups.splice(0, this.groups.length, ...groups);
    }

    private cleanQueue(): void {
        this._callQueue = this.callQueue.filter((call: RdioScannerCall) => {
            return this.selection && this.selection[call.system] && this.selection[call.system][call.talkgroup];
        });
    }

    private download(call: RdioScannerCall = {}): void {
        if (this.config.allowDownload && Array.isArray(call.audio && call.audio.data)) {
            let name;

            if (call.audioName) {
                name = call.audioName;

            } else {
                const sysName = (call.systemData && call.systemData.name) || call.system;
                const tgName = (call.talkgroupData && call.talkgroupData.description) || call.talkgroup;
                const time = new Date(call.startTime).toISOString();

                name = call.audioName || `${sysName}-${tgName}-${time}.m4a`;
            }

            const audio = call.audio.data.reduce((str, val) => str += String.fromCharCode(val), '');
            const type = call.audioType || 'audio/aac';
            const uri = `data:${type};base64,${btoa(audio)}`;

            this.downloadLink.nativeElement.download = name;
            this.downloadLink.nativeElement.href = uri;

            this.downloadLink.nativeElement.click();
        }
    }

    private formatFrequency(frequency: number): string {
        return (typeof frequency === 'number' ? frequency : 0)
            .toString()
            .padStart(9, '0')
            .replace(/(\d)(?=(\d{3})+$)/g, '$1 ')
            .concat(' Hz');
    }

    private async getConfig(): Promise<RdioScannerConfig> {
        const query = await this.appRdioScannerConfigQuery.fetch().toPromise();
        return query.data.rdioScannerConfig;
    }

    private async loadCall(call: RdioScannerCall = {}): Promise<RdioScannerCall | null> {
        const query = await this.appRdioScannerCallQuery.fetch({ id: call.id }).toPromise();

        Object.assign(call, query.data.rdioScannerCall);

        return call;
    }

    private async loadStoredCalls(filters?: any): Promise<void> {
        const limit = this.searchResultsBuffer.length;

        const pageFrom = this.matPaginator.pageIndex * this.matPaginator.pageSize;

        const pageTo = this.matPaginator.pageIndex * this.matPaginator.pageSize + this.matPaginator.pageSize - 1;

        if (!this.searchResultsPending && (filters || !pageFrom ||
            pageFrom < this.searchResultsBufferFrom || pageTo > this.searchResultsBufferTo)) {

            filters = filters || this.searchForm.value;

            this.searchResultsBufferFrom = Math.floor(pageFrom / limit) * limit;
            this.searchResultsBufferTo = this.searchResultsBufferFrom + limit - 1;

            const options: {
                date?: Date;
                limit?: number;
                offset?: number;
                sort?: number;
                system?: number;
                talkgroup?: number;
            } = {};

            if (filters.date instanceof Date) {
                options.date = filters.date;
            }

            if (limit) {
                options.limit = limit;
            }

            if (this.searchResultsBufferFrom) {
                options.offset = this.searchResultsBufferFrom;
            }

            if (filters.sort < 0) {
                options.sort = -1;
            }

            if (filters.system >= 0) {
                options.system = filters.system;
            }

            if (filters.talkgroup >= 0) {
                options.talkgroup = filters.talkgroup;
            }

            this._searchResultsPending = true;

            this.searchForm.disable();

            const query = await this.appRdioScannerCallsQuery.fetch(options, { fetchPolicy: 'no-cache' }).toPromise();

            for (let i = 0; i < limit; i++) {
                this.searchResultsBuffer[i] = this.transformCall(query.data.rdioScannerCalls.results[i]);
            }

            this._searchResultsCount = query.data.rdioScannerCalls.count;
            this._searchFormDateStart = query.data.rdioScannerCalls.dateStart;
            this._searchFormDateStop = query.data.rdioScannerCalls.dateStop;

            this._searchResultsPending = false;

            this.searchForm.enable();
        }

        this.searchResults.next(this.searchResultsBuffer.slice(pageFrom % limit, pageTo % limit + 1));
    }

    private async subscribeLivefeed(): Promise<void> {
        if (!this.livefeedSubscription) {
            const selection = JSON.stringify(this.selection);

            this.livefeedSubscription = this.appRdioScannerCallSubscription.subscribe({ selection })
                .subscribe(({ data }) => this.processCall(data.rdioScannerCall));
        }
    }

    private processCall(call: RdioScannerCall): void {
        if (call !== null && typeof call === 'object') {
            call = this.transformCall(call);
            this.queue(call);
        }
    }

    private processSystems(systems: RdioScannerSystem[]): void {
        if (Array.isArray(systems)) {
            this.systems.splice(0, this.systems.length, ...systems);

            this.readSelection();
            this.rebuildSelection();
            this.writeSelection();

            this.buildGroups();
        }
    }

    private readSelection(): any {
        if (window instanceof Window && window.localStorage instanceof Storage) {
            try {
                this._selection = JSON.parse(window.localStorage.getItem(LOCAL_STORAGE_KEY));
            } catch (err) {
                this._selection = {};
            }
        }
    }

    private rebuildSelection(): void {
        this._selection = this.systems.reduce((sa, sv) => {
            const tgs = sv.talkgroups.map((tg) => tg.dec.toString());

            if (Array.isArray(sa[sv.system])) {
                Object.keys(sa[sv.system]).forEach((tg) => {
                    if (!tgs.includes(tg)) {
                        delete sa[sv.system][tg];
                    }
                });
            }

            sa[sv.system] = sv.talkgroups.reduce((ta, tv) => {
                ta[tv.dec] = typeof ta[tv.dec] === 'boolean' ? ta[tv.dec] : true;
                return ta;
            }, sa[sv.system] || {});
            return sa;
        }, this.selection || {});
    }

    private subscribeSearchForm(): void {
        let lastValue = this.searchForm.value;

        if (!this.searchFormSubscription) {
            this.searchFormSubscription = this.searchForm.valueChanges.subscribe((value) => {
                if (!Object.keys(value).every((key) => value[key] === lastValue[key])) {
                    if (value.system !== lastValue.system && value.talkgroup !== -1) {
                        lastValue.talkgroup = -1;
                        this.searchForm.get('talkgroup').reset(lastValue.talkgroup);
                    }

                    this.matPaginator.firstPage();

                    this.loadStoredCalls(value);

                    lastValue = value;
                }
            });
        }
    }

    private subscribeSearchPaginator(): void {
        if (!this.searchPaginatorSubscription) {
            this.searchPaginatorSubscription = this.matPaginator.page.subscribe(() => this.loadStoredCalls());
        }
    }

    private async subscribeSystems(): Promise<void> {
        if (!this.systemsSubscription) {
            this.appRdioScannerSystemsQuery.watch().valueChanges
                .subscribe(({ data }) => this.processSystems(data.rdioScannerSystems));

            this.systemsSubscription = this.appRdioScannerSystemsSubscription.subscribe()
                .subscribe(({ data }) => this.processSystems(data.rdioScannerSystems));
        }
    }

    private transformCall(call: RdioScannerCall): RdioScannerCall {
        if (call) {
            const system = this.systems.find((_system: RdioScannerSystem) => _system.system === call.system) || {};

            if (Array.isArray(call.srcList)) {
                const aliases = system.aliases || [];

                call.srcList = call.srcList.map((src) => {
                    const alias = aliases.find((_alias) => _alias.uid === src.src);

                    if (alias) {
                        src.name = alias.name;
                    }

                    return src;
                });
            }

            call.systemData = system;

            call.talkgroupData = (call.systemData && Array.isArray(call.systemData.talkgroups) && call.systemData.talkgroups
                .find((talkgroup: RdioScannerTalkgroup) => talkgroup.dec === call.talkgroup)) || {};
        }

        return call;
    }

    private unsubscribeSearchForm(): void {
        if (this.searchFormSubscription) {
            this.searchFormSubscription.unsubscribe();
            this.searchFormSubscription = null;
        }
    }

    private unsubscribeSearchPaginator(): void {
        if (this.searchPaginatorSubscription) {
            this.searchPaginatorSubscription.unsubscribe();
            this.searchPaginatorSubscription = null;
        }
    }

    private unsubscribeLivefeed(): void {
        if (this.livefeedSubscription) {
            this.livefeedSubscription.unsubscribe();
            this.livefeedSubscription = null;
        }
    }

    private unsubscribeSystems(): void {
        if (this.systemsSubscription) {
            this.systemsSubscription.unsubscribe();
            this.systemsSubscription = null;
        }
    }

    private updateDisplay(): void {
        if (this.call) {
            if (isNaN(this.audioStartTime)) {
                this.audioStartTime = isNaN(this.audioContext.currentTime) ? Date.now() : this.audioContext.currentTime;
            }

            const diffTime = this.audioContext.currentTime - this.audioStartTime;
            const currentFreq = this.call.freqList.reduce((r, v) => v.pos <= diffTime ? v : r, {});
            const currentSrc = this.call.srcList.reduce((r, v) => v.pos <= diffTime ? v : r, {});

            this._displayAlphaTag = this.call.talkgroupData.alphaTag || '';
            this._displayDescription = this.call.talkgroupData.description || this.formatFrequency(this.call.freq);
            this._displayErrorCount = currentFreq.errorCount || 0;
            this._displayFrequency = this.formatFrequency(currentFreq.freq || this.call.freq);
            this._displayMode = this.call.talkgroupData.mode || 'A';
            this._displayProgress = new Date(this.call.startTime);
            this._displayProgress.setSeconds(this.displayProgress.getSeconds() + this.audioContext.currentTime - this.audioStartTime);
            this._displaySpikeCount = currentFreq.spikeCount || 0;
            this._displaySystem = this.call.systemData.name || '';
            this._displayTag = this.call.talkgroupData.tag || '';
            this._displayTalkgroup = this.call.talkgroup || 0;
            this._displayUnit = currentSrc.name || currentSrc.src || 0;
        }
    }

    private writeSelection(): any {
        if (window instanceof Window && window.localStorage instanceof Storage) {
            window.localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(this.selection));
        }
    }
}
