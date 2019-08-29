import { Component, OnDestroy, OnInit, ViewChild } from '@angular/core';
import { FormBuilder, FormGroup } from '@angular/forms';
import { MatPaginator, PageEvent } from '@angular/material/paginator';
import { MatTable } from '@angular/material/table';
import { MeteorObservable } from 'meteor-rxjs';
import { BehaviorSubject, combineLatest, Subscription } from 'rxjs';
import { debounceTime, finalize } from 'rxjs/operators';
import { RadioCall, RadioCalls, RadioEvent, RadioSystem, RadioTalkgroup } from '../radio';
import { AppRadioService } from '../radio.service';

const DEFAULT_FILTERS = {
    date: null,
    sort: -1,
    system: -1,
    talkgroup: -1,
};

@Component({
    selector: 'app-radio-search',
    styleUrls: [
        '../radio.scss',
        './radio-search.component.scss',
    ],
    templateUrl: './radio-search.component.html',
})
export class AppRadioSearchComponent implements OnDestroy, OnInit {
    calls: RadioCall[] = [];

    count = 0;

    dateMax: Date;
    dateMin: Date;

    form: FormGroup = this.ngFormBuilder.group({
        date: [],
        sort: [],
        system: [],
        talkgroup: [],
    });

    systems: RadioSystem[] = [];

    talkgroups: RadioTalkgroup[] = [];

    readonly columns = ['control', 'date', 'time', 'system', 'alpha', 'description'];

    get actualPageSize() {
        return this.pageSize.value;
    }

    @ViewChild(MatPaginator, { static: true }) private matPaginator: MatPaginator;
    @ViewChild(MatTable, { static: true }) private matTable: MatTable<RadioCall[]>;

    private active = false;
    private filterDate: BehaviorSubject<Date> = new BehaviorSubject<Date>(DEFAULT_FILTERS.date);
    private filterSystem: BehaviorSubject<number> = new BehaviorSubject<number>(DEFAULT_FILTERS.system);
    private filterTalkgroup: BehaviorSubject<number> = new BehaviorSubject<number>(DEFAULT_FILTERS.talkgroup);
    private pageCurrent: BehaviorSubject<number> = new BehaviorSubject<number>(1);
    private pageSize: BehaviorSubject<number> = new BehaviorSubject<number>(10);
    private pageSort: BehaviorSubject<number> = new BehaviorSubject<number>(DEFAULT_FILTERS.sort);

    private subscriptions: {
        calls: Subscription[];
        dateLimits: Subscription[];
        form: Subscription[];
        radio: Subscription[];
    } = {
            calls: [],
            dateLimits: [],
            form: [],
            radio: [],
        };

    constructor(public appRadioService: AppRadioService, public ngFormBuilder: FormBuilder) {
        this.resetFilters();

        if (this.form.get('system').value === -1) {
            this.form.get('talkgroup').disable();
        }
    }

    ngOnInit(): void {
        this.subscribeFormChanges();
        this.subscribeRadioEvent();
    }

    ngOnDestroy(): void {
        this.unsubscribe();
    }

    close(): void {
        this.appRadioService.search();
    }

    onPage(event: PageEvent): void {
        this.pageCurrent.next(event.pageIndex + 1);
    }

    play(call: RadioCall): void {
        if (call) {
            if (call.audio) {
                this.appRadioService.play(call);
            } else {
                this.loadAudio(call).then(() => this.appRadioService.play(call));
            }
        }
    }

    resetFilters(): void {
        this.form.reset(DEFAULT_FILTERS);
    }

    private handleSearchEvent(event: RadioEvent): void {
        if ('search' in event) {
            this.active = !this.active;

            if (this.active) {
                this.subscribeCalls();
                this.subscribeDateLimits();
            } else {
                this.unsubscribeCalls();
                this.unsubscribeDateLimits();
            }
        }
    }

    private handleSystemsEvent(event: RadioEvent): void {
        if ('systems' in event) {
            this.systems.splice(0, this.systems.length, ...event.systems);
        }
    }

    private loadAudio(call: RadioCall): Promise<RadioCall> {
        return new Promise((resolve) => {
            if (call) {
                MeteorObservable.call('call-audio', call._id).subscribe((audio: string) => {
                    call.audio = audio;
                    resolve(call);
                });
            } else {
                resolve(call);
            }
        });
    }

    private subscribeCalls(): void {
        const subscriptions: Subscription[] = [];

        this.subscriptions.calls.push(combineLatest(
            this.filterDate,
            this.filterSystem,
            this.filterTalkgroup,
            this.pageCurrent,
            this.pageSize,
            this.pageSort,
        )
            .pipe(
                debounceTime(100),
                finalize(() => this.unsubscribe(subscriptions)),
            )
            .subscribe(([
                date,
                system,
                talkgroup,
                current,
                size,
                sort,
            ]) => {
                const selector: any = {};

                if (date instanceof Date) {
                    selector.$and = [
                        {
                            startTime: {
                                $gte: new Date(date.getFullYear(), date.getMonth(), date.getDate()),
                            },
                        },
                        {
                            startTime: {
                                $lt: new Date(date.getFullYear(), date.getMonth(), date.getDate() + 1),
                            },
                        },
                    ];
                }

                if (system !== -1) {
                    selector.system = system;
                }

                if (talkgroup !== -1) {
                    selector.talkgroup = talkgroup;
                }

                this.unsubscribe(subscriptions);

                subscriptions.push(MeteorObservable.subscribe('calls', selector, {
                    fields: {
                        audio: 0,
                    },
                    limit: size,
                    skip: (current - 1) * size,
                    sort: { startTime: sort },
                }).subscribe(() => {
                    subscriptions.push(RadioCalls
                        .find(selector, {
                            limit: size,
                            sort: { startTime: sort },
                            transform: (call: RadioCall) => this.appRadioService.transform(call),
                        })
                        .pipe(debounceTime(100))
                        .subscribe((calls: RadioCall[]) => {
                            this.calls.splice(0, this.calls.length, ...calls);

                            this.matTable.renderRows();

                            MeteorObservable.call('calls-count', selector).subscribe((count: number) => this.count = count);
                        }));
                }));
            }));
    }

    private subscribeDateLimits(): void {
        this.subscriptions.dateLimits.push(

            MeteorObservable.subscribe('calls', {}, {
                fields: { audio: 0 },
                limit: 1,
                sort: { startTime: -1 },
            }).subscribe(() => {
                const call = RadioCalls.findOne({}, {
                    sort: { startTime: -1 },
                });

                if (call) {
                    this.dateMax = call.startTime;
                }
            }),

            MeteorObservable.subscribe('calls', {}, {
                fields: { audio: 0 },
                limit: 1,
                sort: { startTime: 1 },
            }).subscribe(() => {
                const call = RadioCalls.findOne({}, {
                    sort: { startTime: 1 },
                });

                if (call) {
                    this.dateMin = call.startTime;
                }
            }),

        );
    }

    private subscribeFormChanges(): void {
        const dateForm = this.form.get('date');
        const sortForm = this.form.get('sort');
        const systemForm = this.form.get('system');
        const talkgroupForm = this.form.get('talkgroup');

        this.subscriptions.form.push(dateForm.valueChanges.subscribe((date: Date) => {
            this.filterDate.next(date);
        }));

        this.subscriptions.form.push(systemForm.valueChanges.subscribe((sys: number) => {
            if (sys === -1) {
                this.talkgroups.splice(0, this.talkgroups.length);

                if (talkgroupForm.enabled) {
                    talkgroupForm.disable();
                }

            } else {
                const system = this.systems.find((_system: RadioSystem) => _system.system === sys);

                if (system) {
                    this.talkgroups.splice(0, this.talkgroups.length, ...system.talkgroups);
                }

                if (talkgroupForm.disabled) {
                    talkgroupForm.enable();
                }
            }

            talkgroupForm.reset(-1);

            this.filterSystem.next(sys);
            this.matPaginator.firstPage();
        }));

        this.subscriptions.form.push(talkgroupForm.valueChanges.subscribe((tg: number) => {
            this.filterTalkgroup.next(tg);
            this.matPaginator.firstPage();
        }));

        this.subscriptions.form.push(sortForm.valueChanges.subscribe((sort: number) => {
            this.pageSort.next(sort);
            this.matPaginator.firstPage();
        }));
    }

    private subscribeRadioEvent(): void {
        this.subscriptions.radio.push(this.appRadioService.event.subscribe((event: RadioEvent) => {
            this.handleSearchEvent(event);
            this.handleSystemsEvent(event);
        }));
    }

    private unsubscribe(subscriptions: Subscription[] = [
        ...this.subscriptions.calls,
        ...this.subscriptions.dateLimits,
        ...this.subscriptions.form,
        ...this.subscriptions.radio,
    ]): void {
        while (subscriptions.length) {
            subscriptions.pop().unsubscribe();
        }
    }

    private unsubscribeCalls(): void {
        this.unsubscribe(this.subscriptions.calls);
    }

    private unsubscribeDateLimits(): void {
        this.unsubscribe(this.subscriptions.dateLimits);
    }
}
