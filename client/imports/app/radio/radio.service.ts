import { EventEmitter, Injectable, OnDestroy } from '@angular/core';
import { MeteorObservable } from 'meteor-rxjs';
import { Subject, Subscription } from 'rxjs';
import { debounceTime, filter, skip } from 'rxjs/operators';
import { RadioAvoids, RadioCall, RadioCalls, RadioEvent, RadioSystem, RadioSystems, RadioTalkgroup } from './radio';

const LOCAL_STORAGE_KEY = {
    avoids: 'radio-avoids',
};

@Injectable()
export class AppRadioService implements OnDestroy {
    readonly event = new EventEmitter<RadioEvent>();

    get live() {
        return !!this._subscriptions.liveFeed.length;
    }

    private _audio: HTMLAudioElement;
    private _audioEvent: Subject<void> = new Subject();

    private _avoids: RadioAvoids = {};

    private _call: {
        current: RadioCall | null;
        previous: RadioCall | null;
        queue: RadioCall[];
    } = {
            current: null,
            previous: null,
            queue: [],
        };

    private _hold: {
        system: RadioSystem | number | null,
        talkgroup: RadioTalkgroup | number | null,
    } = {
            system: null,
            talkgroup: null,
        };

    private _subscriptions: {
        audio: Subscription[];
        liveFeed: Subscription[];
        systems: Subscription[];
    } = {
            audio: [],
            liveFeed: [],
            systems: [],
        };

    private _systems: RadioSystem[] = [];

    constructor() {
        this._readAvoids();

        this._subscribeAudioEvent();
        this._subscribeSystems();
    }

    ngOnDestroy(): void {
        this.event.complete();

        this._unsubscribe();
    }

    avoid(parms: {
        all?: boolean;
        call?: RadioCall;
        system?: RadioSystem;
        talkgroup?: RadioTalkgroup;
        status?: boolean
    } = {}): void {
        if (typeof parms.all === 'boolean') {
            Object.keys(this._avoids).map((sys: string) => +sys).forEach((sys: number) => {
                Object.keys(this._avoids[sys]).map((tg: string) => +tg).forEach((tg: number) => {
                    this._avoids[sys][tg] = typeof parms.status === 'boolean' ? parms.status : !this._avoids[sys][tg];
                    this._emitAvoidEvent(sys, tg);
                });
            });

        } else if (parms.call) {
            const sys = parms.call.system;
            const tg = parms.call.talkgroup;
            this._avoids[sys][tg] = typeof parms.status === 'boolean' ? parms.status : !this._avoids[sys][tg];
            this._emitAvoidEvent(sys, tg);

        } else if (parms.system && parms.talkgroup) {
            const sys = parms.system.system;
            const tg = parms.talkgroup.dec;
            this._avoids[sys][tg] = typeof parms.status === 'boolean' ? parms.status : !this._avoids[sys][tg];
            this._emitAvoidEvent(sys, tg);

        } else if (parms.system && !parms.talkgroup) {
            const sys = parms.system.system;
            Object.keys(this._avoids[sys]).map((tg: string) => +tg).forEach((tg: number) => {
                this._avoids[sys][tg] = typeof parms.status === 'boolean' ? parms.status : !this._avoids[sys][tg];
                this._emitAvoidEvent(sys, tg);
            });

        } else {
            const call = this._call.current || this._call.previous;

            if (call) {
                const sys = call.system;
                const tg = call.talkgroup;
                this._avoids[sys][tg] = typeof parms.status === 'boolean' ? parms.status : !this._avoids[sys][tg];
                this._emitAvoidEvent(sys, tg);
                this._writeAvoids();
            }
        }

        if (this._call.current && this._callFiltered()) {
            this.stop();
        }

        this._cleanQueue();

        if (this.live) {
            this._unsubscribeLiveFeed();
            this._subscribeLiveFeed();
        }

        this._writeAvoids();
    }

    getAvoids(): RadioAvoids {
        return this._avoids;
    }

    holdSystem(): void {
        const call = this._call.current || this._call.previous;

        if (call) {
            this._hold.system = this._hold.system === null ? call.systemData || call.system : null;

            this._emitHoldEvent(this._hold.system === null ? '-sys' : '+sys');

            if (this._hold.system !== null) {
                this._cleanQueue();
            }
        }
    }

    holdTalkgroup(): void {
        const call = this._call.current || this._call.previous;

        if (call) {
            this._hold.talkgroup = this._hold.talkgroup === null ? call.talkgroupData || call.talkgroup : null;

            this._emitHoldEvent(this._hold.talkgroup === null ? '-tg' : '+tg');

            if (this._hold.talkgroup !== null) {
                this._cleanQueue();
            }
        }
    }

    liveFeed(status = !this.live): void {
        this._configureAudio();

        if (status) {
            this._subscribeLiveFeed();

        } else {
            this._unsubscribeLiveFeed();

            this._emitLiveEvent();

            this._flushQueue();

            this.stop();
        }
    }

    play(call?: RadioCall): void {
        this._configureAudio();

        if (call) {
            if (call.audio) {
                if (this._call.current) {
                    this._call.queue.unshift(call);
                    this.stop();
                } else {
                    this._call.current = call;

                    this._audio.src = call.audio;
                    this._audio.load();

                    this._emitCallEvent();
                }
            }
        } else if (!this._call.current && this._call.queue.length) {
            this._call.current = this._call.queue.shift();

            this._audio.src = this._call.current.audio;
            this._audio.load();

            this._emitCallEvent();
            this._emitQueueEvent();
        }
    }

    queue(call: RadioCall | RadioCall[], bypass = false): void {
        if (Array.isArray(call)) {
            call.forEach((_call: RadioCall) => this.queue(_call));

        } else if (bypass || !this._callFiltered(call)) {
            this._call.queue.push(call);

            this._emitQueueEvent();

            this.play();
        }
    }

    replay(): void {
        const call = this._call.current || this._call.previous;

        if (call) {
            this.play(call);
        }
    }

    search(): void {
        this._emitSearchEvent();
    }

    select(): void {
        this._emitSelectEvent();
    }

    stop(): void {
        if (this._call.current) {
            this._audio.pause();

            this._audio.src = null;
            this._audio.load();
        }
    }

    transform(call: RadioCall): RadioCall {
        call.systemData = (call && this._systems
            .find((_system: RadioSystem) => _system.system === call.system)) || null;

        call.talkgroupData = (call && call.systemData && call.systemData.talkgroups
            .find((_talkgroup: RadioTalkgroup) => _talkgroup.dec === call.talkgroup)) || null;

        return call;
    }

    private _callFiltered(call: RadioCall = this._call.current): boolean {
        if (
            this._hold.system !== null &&
            (typeof this._hold.system === 'object' && this._hold.system !== call.systemData) ||
            (typeof this._hold.system === 'number' && this._hold.system !== call.system)
        ) {
            return true;

        } else if (
            this._hold.talkgroup !== null &&
            (typeof this._hold.talkgroup === 'object' && this._hold.talkgroup !== call.talkgroupData) ||
            (typeof this._hold.talkgroup === 'number' && this._hold.talkgroup !== call.talkgroup)
        ) {
            return true;

        } else {
            const sys = call.system;
            const tg = call.talkgroup;
            const avoided = this._avoids[sys] && this._avoids[sys][tg];
            return typeof avoided === 'boolean' ? avoided : false;
        }
    }

    private _cleanQueue(): void {
        const queueLength = this._call.queue.length;

        this._call.queue = this._call.queue.filter((_call: RadioCall) => !this._callFiltered(_call));

        if (queueLength !== this._call.queue.length) {
            this._emitQueueEvent();
        }
    }

    private _configureAudio(): void {
        if (!this._audio) {
            this._audio = new Audio();

            this._audio.oncanplaythrough = () => this._audio.play();

            this._audio.onabort = () => this._audioEvent.next();
            this._audio.onended = () => this._audioEvent.next();
            this._audio.onerror = () => this._audioEvent.next();
            this._audio.onpause = () => this._audioEvent.next();
            this._audio.onstalled = () => this._audioEvent.next();

            this._audio.ontimeupdate = () => this._emitTimeEvent();
        }
    }

    private _emitAvoidEvent(sys: number, tg: number): void {
        this.event.emit({ avoid: { sys, tg, status: this._avoids[sys][tg] } });
    }

    private _emitAvoidsEvent(): void {
        this.event.emit({ avoids: this._avoids });
    }

    private _emitCallEvent(): void {
        this.event.emit({ call: this._call.current });
    }

    private _emitHoldEvent(value: RadioEvent['hold']): void {
        this.event.emit({ hold: value });
    }

    private _emitLiveEvent(): void {
        this.event.emit({ live: this.live });
    }

    private _emitQueueEvent(): void {
        this.event.emit({ queue: this._call.queue.length });
    }

    private _emitSearchEvent(): void {
        this.event.emit({ search: null });
    }

    private _emitSelectEvent(): void {
        this.event.emit({ select: null });
    }

    private _emitSystemsEvent(): void {
        this.event.emit({ systems: this._systems });
    }

    private _emitTimeEvent(): void {
        this.event.emit({ time: this._audio.currentTime });
    }

    private _flushQueue() {
        this._call.queue.splice(0, this._call.queue.length);

        this._emitQueueEvent();
    }

    private _readAvoids(): void {
        const avoids = this._readLocalStorage(LOCAL_STORAGE_KEY.avoids);

        if (avoids) {
            Object.keys(avoids).map((sys: string) => +sys).forEach((sys: number) => {
                if (Array.isArray(avoids[sys])) {
                    avoids[sys].forEach((tg: number) => {
                        if (!this._avoids[sys]) {
                            this._avoids[sys] = {};
                        }

                        this._avoids[sys][tg] = true;
                    });
                }
            });
        }

        this._emitAvoidsEvent();
    }

    private _readLocalStorage(key: string): any {
        if (window instanceof Window && window.localStorage instanceof Storage) {
            try {
                return JSON.parse(window.localStorage.getItem(key));
            } catch (e) {
                return null;
            }
        }
    }

    private _subscribeAudioEvent(): void {
        this._subscriptions.audio.push(
            this._audioEvent
                .pipe(debounceTime(1000))
                .subscribe(() => {
                    if (this._call.current) {
                        this._call.previous = this._call.current;
                        this._call.current = null;

                        this._emitCallEvent();

                        setTimeout(() => this.play(), 1000);
                    }
                }),
        );
    }

    private _subscribeLiveFeed(): void {
        if (!this._subscriptions.liveFeed.length) {
            const options = {
                limit: 1,
                sort: {
                    createdAt: -1
                },
                transform: (call: RadioCall) => this.transform(call),
            };

            const avoids = Object.keys(this._avoids).map((sys: string) => +sys).reduce((sel: any, sys: number) => {
                const tgs = Object.keys(this._avoids[sys]).map((tg: string) => +tg).filter((tg: number) => this._avoids[sys][tg]);

                if (tgs.length) {
                    sel.push({
                        $and: [
                            {
                                system: { $ne: sys },
                            },
                            {
                                talkgroup: { $nin: tgs },
                            },
                        ],
                    });
                }

                return sel;
            }, []);

            const selector = avoids.length ? { $or: avoids } : {};

            this._subscriptions.liveFeed.push(MeteorObservable
                .subscribe('calls', selector, options)
                .subscribe(() => {
                    this._subscriptions.liveFeed.push(RadioCalls
                        .find(selector, options)
                        .pipe(
                            filter((calls: RadioCall[]) => !!calls.length),
                            skip(2),
                        )
                        .subscribe((calls: RadioCall[]) => this.queue(calls)));
                }));

            this._emitLiveEvent();
        }
    }

    private _subscribeSystems(): Promise<void> {
        let hasInitialize = false;

        return new Promise((resolve) => {
            if (!this._subscriptions.systems.length) {
                this._subscriptions.systems.push(MeteorObservable
                    .subscribe('systems')
                    .subscribe());

                this._subscriptions.systems.push(RadioSystems
                    .find({}, {
                        sort: {
                            system: 1,
                        },
                    })
                    .pipe(debounceTime(100))
                    .subscribe((systems: RadioSystem[]) => {
                        if (systems.length) {
                            this._systems.splice(0, this._systems.length, ...systems);

                            this._emitSystemsEvent();

                            this._syncAvoids();
                            this._writeAvoids();

                            if (!hasInitialize) {
                                hasInitialize = true;
                                resolve();
                            }
                        }
                    }));
            }
        });
    }

    private _syncAvoids(): void {
        this._systems.forEach((system: RadioSystem) => {
            const sys = system.system;

            if (typeof this._avoids[sys] !== 'object') {
                this._avoids[sys] = {};
            }

            system.talkgroups.forEach((talkgroup: RadioTalkgroup) => {
                const tg = talkgroup.dec;

                if (typeof this._avoids[sys][tg] !== 'boolean') {
                    this._avoids[sys][tg] = false;
                }
            });
        });

        Object.keys(this._avoids).map((sys: string) => +sys).forEach((sys: number) => {
            const system = this._systems.find((_system: RadioSystem) => _system.system === +sys);

            if (system) {
                Object.keys(this._avoids[sys]).map((tg: string) => +tg).forEach((tg: number) => {
                    const talkgroup = system.talkgroups.find((_talkgroup: RadioTalkgroup) => _talkgroup.dec === +tg);

                    if (!talkgroup) {
                        delete this._avoids[sys][tg];
                    }
                });

            } else {
                delete this._avoids[sys];
            }
        });
    }

    private _unsubscribe(subscriptions: Subscription[] = [
        ...this._subscriptions.audio,
        ...this._subscriptions.liveFeed,
        ...this._subscriptions.systems,
    ]) {
        while (subscriptions.length) {
            subscriptions.pop().unsubscribe();
        }
    }

    private _unsubscribeLiveFeed(): void {
        this._unsubscribe(this._subscriptions.liveFeed);
    }

    private _writeAvoids(): void {
        const avoids: { [sys: number]: number[] } = {};

        Object.keys(this._avoids).map((sys: string) => +sys).forEach((sys: number) => {
            Object.keys(this._avoids[sys]).map((tg: string) => +tg).forEach((tg: number) => {
                if (this._avoids[sys][tg]) {
                    if (Array.isArray(avoids[sys])) {
                        avoids[sys].push(tg);
                    } else {
                        avoids[sys] = [tg];
                    }
                }
            });
        });

        this._writeLocalStorage(LOCAL_STORAGE_KEY.avoids, avoids);
    }

    private _writeLocalStorage(key: string, value: any): void {
        if (window instanceof Window && window.localStorage instanceof Storage) {
            window.localStorage.setItem(key, JSON.stringify(value));
        }
    }
}
