import { EventEmitter, Injectable, OnDestroy } from '@angular/core';
import { MeteorObservable } from 'meteor-rxjs';
import { Subscription } from 'rxjs';
import { debounceTime, filter, skip } from 'rxjs/operators';
import { RadioAvoids, RadioCall, RadioCalls, RadioEvent, RadioSystem, RadioSystems, RadioTalkgroup } from './radio';

declare var webkitAudioContext: any;

const EVENTS = ['mousedown', 'touchdown'];

const LOCAL_STORAGE_KEY = {
    avoids: 'radio-avoids',
};

@Injectable()
export class AppRadioService implements OnDestroy {
    readonly event = new EventEmitter<RadioEvent>();

    get live() {
        return !!this.subscriptions.liveFeed.length;
    }

    private audioContext: AudioContext;

    private audioSource: AudioBufferSourceNode = null;

    private audioStartTime = NaN;

    private audioTimer: any = null;

    private avoids: RadioAvoids = {};

    private call: {
        current: RadioCall | null;
        previous: RadioCall | null;
        queue: RadioCall[];
    } = {
            current: null,
            previous: null,
            queue: [],
        };

    private hold: {
        system: RadioSystem | number | null,
        talkgroup: RadioTalkgroup | number | null,
    } = {
            system: null,
            talkgroup: null,
        };

    private paused = false;

    private subscriptions: {
        liveFeed: Subscription[];
        systems: Subscription[];
    } = {
            liveFeed: [],
            systems: [],
        };

    private systems: RadioSystem[] = [];

    constructor() {
        this.bootstrap();

        this.readAvoids();

        this.subscribeSystems();
    }

    ngOnDestroy(): void {
        this.event.complete();

        this.unsubscribe();
    }

    avoid(parms: {
        all?: boolean;
        call?: RadioCall;
        system?: RadioSystem;
        talkgroup?: RadioTalkgroup;
        status?: boolean
    } = {}): void {
        if (typeof parms.all === 'boolean') {
            Object.keys(this.avoids).map((sys: string) => +sys).forEach((sys: number) => {
                Object.keys(this.avoids[sys]).map((tg: string) => +tg).forEach((tg: number) => {
                    this.avoids[sys][tg] = typeof parms.status === 'boolean' ? parms.status : !this.avoids[sys][tg];
                    this.emitAvoidEvent(sys, tg);
                });
            });

        } else if (parms.call) {
            const sys = parms.call.system;
            const tg = parms.call.talkgroup;
            this.avoids[sys][tg] = typeof parms.status === 'boolean' ? parms.status : !this.avoids[sys][tg];
            this.emitAvoidEvent(sys, tg);

        } else if (parms.system && parms.talkgroup) {
            const sys = parms.system.system;
            const tg = parms.talkgroup.dec;
            this.avoids[sys][tg] = typeof parms.status === 'boolean' ? parms.status : !this.avoids[sys][tg];
            this.emitAvoidEvent(sys, tg);

        } else if (parms.system && !parms.talkgroup) {
            const sys = parms.system.system;
            Object.keys(this.avoids[sys]).map((tg: string) => +tg).forEach((tg: number) => {
                this.avoids[sys][tg] = typeof parms.status === 'boolean' ? parms.status : !this.avoids[sys][tg];
                this.emitAvoidEvent(sys, tg);
            });

        } else {
            const call = this.call.current || this.call.previous;

            if (call) {
                const sys = call.system;
                const tg = call.talkgroup;
                this.avoids[sys][tg] = typeof parms.status === 'boolean' ? parms.status : !this.avoids[sys][tg];
                this.emitAvoidEvent(sys, tg);
            }
        }

        this.cleanQueue();

        if (this.call.current && this.callFiltered()) {
            this.skip();
        }

        if (this.live) {
            this.unsubscribeLiveFeed();
            this.subscribeLiveFeed();
        }

        this.writeAvoids();
    }

    getAvoids(): RadioAvoids {
        return this.avoids;
    }

    holdSystem(): void {
        const call = this.call.current || this.call.previous;

        if (call) {
            this.hold.system = this.hold.system === null ? call.systemData || call.system : null;

            this.emitHoldEvent(this.hold.system === null ? '-sys' : '+sys');

            if (this.hold.system !== null) {
                this.cleanQueue();
            }
        }
    }

    holdTalkgroup(): void {
        const call = this.call.current || this.call.previous;

        if (call) {
            this.hold.talkgroup = this.hold.talkgroup === null ? call.talkgroupData || call.talkgroup : null;

            this.emitHoldEvent(this.hold.talkgroup === null ? '-tg' : '+tg');

            if (this.hold.talkgroup !== null) {
                this.cleanQueue();
            }
        }
    }

    liveFeed(status = !this.live): void {
        if (status) {
            this.subscribeLiveFeed();

        } else {
            this.unsubscribeLiveFeed();

            this.emitLiveEvent();

            this.flushQueue();

            this.stop();
        }
    }

    pause(): void {
        this.paused = !this.paused;

        this.emitPauseEvent();

        if (this.audioContext) {
            if (this.paused) {
                this.audioContext.suspend();
            } else {
                this.audioContext.resume();

                this.play();
            }
        }
    }

    play(call?: RadioCall): void {
        if (this.audioContext && !this.paused) {
            if (!call && !this.call.current && this.call.queue.length) {
                call = this.call.queue.shift();
                this.emitQueueEvent();
            }

            if (call && call.audio) {
                this.call.previous = this.call.current;
                this.call.current = call;

                this.emitCallEvent();

                const audio = /^data:[^;]+;base64,/.test(call.audio) ? atob(call.audio.replace(/^[^,]*,/, '')) : call.audio;
                const arrayBuffer = new ArrayBuffer(audio.length);
                const arrayBufferView = new Uint8Array(arrayBuffer);

                for (let i = 0; i < audio.length; i++) {
                    arrayBufferView[i] = audio.charCodeAt(i);
                }

                this.audioContext.decodeAudioData(arrayBuffer, (buffer) => {
                    if (this.audioSource) {
                        this.audioSource.disconnect();
                    }

                    this.audioSource = this.audioContext.createBufferSource();

                    this.audioSource.buffer = buffer;
                    this.audioSource.connect(this.audioContext.destination);
                    this.audioSource.onended = () => this.skip();
                    this.audioSource.start();

                    if (this.audioContext.state === 'suspended') {
                        const resume = () => {
                            this.audioContext.resume();

                            setTimeout(() => {
                                if (this.audioContext.state === 'running') {
                                    EVENTS.forEach((event) => document.body.removeEventListener(event, resume));
                                }
                            }, 0);
                        };

                        EVENTS.forEach((event) => document.body.addEventListener(event, resume));
                    }

                    this.startAudioTimer();

                }, (error: Error) => {
                    this.skip(true);

                    throw (error);
                });
            }
        }
    }


    queue(call: RadioCall | RadioCall[]): void {
        if (Array.isArray(call)) {
            call.forEach((_call: RadioCall) => this.queue(_call));

        } else if (!this.callFiltered(call)) {
            this.call.queue.push(call);

            this.emitQueueEvent();

            this.play();
        }
    }

    replay(): void {
        if (!this.paused) {
            this.stopAudioTimer();

            this.play(this.call.current || this.call.previous);
        }
    }

    search(): void {
        this.emitSearchEvent();
    }

    select(): void {
        this.emitSelectEvent();
    }

    skip(nodelay = false): void {
        this.stop();

        if (nodelay) {
            this.play();

        } else {
            setTimeout(() => this.play(), 1000);
        }
    }

    stop(): void {
        this.stopAudioTimer();

        if (this.audioSource) {
            this.audioSource.disconnect();
            this.audioSource = null;
        }

        this.audioStartTime = NaN;

        if (this.call.current) {
            this.call.previous = this.call.current;
            this.call.current = null;
        }

        this.emitCallEvent();
    }

    transform(call: RadioCall): RadioCall {
        call.systemData = (call && this.systems
            .find((system: RadioSystem) => system.system === call.system)) || null;

        call.talkgroupData = (call && call.systemData && call.systemData.talkgroups
            .find((talkgroup: RadioTalkgroup) => talkgroup.dec === call.talkgroup)) || null;

        return call;
    }

    private bootstrap(): void {
        const bootstrap = () => {
            if (!this.audioContext) {
                if ('webkitAudioContext' in window) {
                    this.audioContext = new webkitAudioContext();
                } else {
                    this.audioContext = new AudioContext();
                }

                if (this.audioContext.state === 'suspended') {
                    this.audioContext.resume();
                }
            }

            EVENTS.forEach((event) => document.body.removeEventListener(event, bootstrap));
        };

        EVENTS.forEach((event) => document.body.addEventListener(event, bootstrap));
    }

    private callFiltered(call: RadioCall = this.call.current): boolean {
        if (
            this.hold.system !== null &&
            (typeof this.hold.system === 'object' && this.hold.system !== call.systemData) ||
            (typeof this.hold.system === 'number' && this.hold.system !== call.system)
        ) {
            return true;

        } else if (
            this.hold.talkgroup !== null &&
            (typeof this.hold.talkgroup === 'object' && this.hold.talkgroup !== call.talkgroupData) ||
            (typeof this.hold.talkgroup === 'number' && this.hold.talkgroup !== call.talkgroup)
        ) {
            return true;

        } else {
            const sys = call.system;
            const tg = call.talkgroup;
            const avoided = this.avoids[sys] && this.avoids[sys][tg];
            return typeof avoided === 'boolean' ? avoided : false;
        }
    }

    private cleanQueue(): void {
        const queueLength = this.call.queue.length;

        this.call.queue = this.call.queue.filter((call: RadioCall) => !this.callFiltered(call));

        if (queueLength !== this.call.queue.length) {
            this.emitQueueEvent();
        }
    }

    private emitAvoidEvent(sys: number, tg: number): void {
        this.event.emit({ avoid: { sys, tg, status: this.avoids[sys][tg] } });
    }

    private emitAvoidsEvent(): void {
        this.event.emit({ avoids: this.avoids });
    }

    private emitCallEvent(): void {
        this.event.emit({ call: this.call.current });
    }

    private emitHoldEvent(value: RadioEvent['hold']): void {
        this.event.emit({ hold: value });
    }

    private emitLiveEvent(): void {
        this.event.emit({ live: this.live });
    }

    private emitPauseEvent(): void {
        this.event.emit({ pause: this.paused });
    }

    private emitQueueEvent(): void {
        this.event.emit({ queue: this.call.queue.length });
    }

    private emitSearchEvent(): void {
        this.event.emit({ search: null });
    }

    private emitSelectEvent(): void {
        this.event.emit({ select: null });
    }

    private emitSystemsEvent(): void {
        this.event.emit({ systems: this.systems });
    }

    private emitTimeEvent(): void {
        if (!this.paused && !isNaN(this.audioContext.currentTime)) {
            if (isNaN(this.audioStartTime)) {
                this.audioStartTime = this.audioContext.currentTime;
            }

            this.event.emit({ time: this.audioContext.currentTime - this.audioStartTime });
        }
    }

    private flushQueue() {
        this.call.queue.splice(0, this.call.queue.length);

        this.emitQueueEvent();
    }

    private readAvoids(): void {
        const avoids = this.readLocalStorage(LOCAL_STORAGE_KEY.avoids);

        if (avoids) {
            Object.keys(avoids).map((sys: string) => +sys).forEach((sys: number) => {
                if (Array.isArray(avoids[sys])) {
                    avoids[sys].forEach((tg: number) => {
                        if (!this.avoids[sys]) {
                            this.avoids[sys] = {};
                        }

                        this.avoids[sys][tg] = true;
                    });
                }
            });
        }

        this.emitAvoidsEvent();
    }

    private readLocalStorage(key: string): any {
        if (window instanceof Window && window.localStorage instanceof Storage) {
            try {
                return JSON.parse(window.localStorage.getItem(key));
            } catch (e) {
                return null;
            }
        }
    }

    private startAudioTimer(): void {
        if (this.audioTimer === null) {
            this.emitTimeEvent();

            this.audioTimer = setInterval(() => this.emitTimeEvent(), 500);
        }
    }

    private stopAudioTimer(): void {
        if (this.audioTimer !== null) {
            clearInterval(this.audioTimer);
            this.audioTimer = null;
        }
    }

    private subscribeLiveFeed(): void {
        if (!this.subscriptions.liveFeed.length) {
            const options = {
                limit: 1,
                sort: {
                    createdAt: -1,
                },
                transform: (call: RadioCall) => this.transform(call),
            };

            const avoids = Object.keys(this.avoids)
                .map((sys: string) => +sys)
                .reduce((sel: any, sys: number) => {
                    const tgs = Object.keys(this.avoids[sys])
                        .map((tg: string) => +tg)
                        .filter((tg: number) => this.avoids[sys][tg]);

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

            this.subscriptions.liveFeed.push(MeteorObservable
                .subscribe('calls', selector, options)
                .subscribe(() => {
                    this.subscriptions.liveFeed.push(RadioCalls
                        .find(selector, options)
                        .pipe(
                            filter((calls: RadioCall[]) => !!calls.length),
                            skip(2),
                        )
                        .subscribe((calls: RadioCall[]) => this.queue(calls)));
                }));

            this.emitLiveEvent();
        }
    }

    private subscribeSystems(): void {
        if (!this.subscriptions.systems.length) {
            this.subscriptions.systems.push(MeteorObservable
                .subscribe('systems')
                .subscribe());

            this.subscriptions.systems.push(RadioSystems
                .find({}, {
                    sort: {
                        system: 1,
                    },
                })
                .pipe(debounceTime(100))
                .subscribe((systems: RadioSystem[]) => {
                    if (systems.length) {
                        this.systems.splice(0, this.systems.length, ...systems);

                        this.emitSystemsEvent();

                        this.syncAvoids();
                        this.writeAvoids();
                    }
                }));
        }
    }

    private syncAvoids(): void {
        this.systems.forEach((system: RadioSystem) => {
            const sys = system.system;

            if (typeof this.avoids[sys] !== 'object') {
                this.avoids[sys] = {};
            }

            system.talkgroups.forEach((talkgroup: RadioTalkgroup) => {
                const tg = talkgroup.dec;

                if (typeof this.avoids[sys][tg] !== 'boolean') {
                    this.avoids[sys][tg] = false;
                }
            });
        });

        Object.keys(this.avoids).map((sys: string) => +sys).forEach((sys: number) => {
            const system = this.systems.find((_system: RadioSystem) => _system.system === +sys);

            if (system) {
                Object.keys(this.avoids[sys]).map((tg: string) => +tg).forEach((tg: number) => {
                    const talkgroup = system.talkgroups.find((_talkgroup: RadioTalkgroup) => _talkgroup.dec === +tg);

                    if (!talkgroup) {
                        delete this.avoids[sys][tg];
                    }
                });

            } else {
                delete this.avoids[sys];
            }
        });
    }

    private unsubscribe(subscriptions: Subscription[] = [
        ...this.subscriptions.liveFeed,
        ...this.subscriptions.systems,
    ]) {
        while (subscriptions.length) {
            subscriptions.pop().unsubscribe();
        }
    }

    private unsubscribeLiveFeed(): void {
        this.unsubscribe(this.subscriptions.liveFeed);
    }

    private writeAvoids(): void {
        const avoids: { [sys: number]: number[] } = {};

        Object.keys(this.avoids).map((sys: string) => +sys).forEach((sys: number) => {
            Object.keys(this.avoids[sys]).map((tg: string) => +tg).forEach((tg: number) => {
                if (this.avoids[sys][tg]) {
                    if (Array.isArray(avoids[sys])) {
                        avoids[sys].push(tg);
                    } else {
                        avoids[sys] = [tg];
                    }
                }
            });
        });

        this.writeLocalStorage(LOCAL_STORAGE_KEY.avoids, avoids);
    }

    private writeLocalStorage(key: string, value: any): void {
        if (window instanceof Window && window.localStorage instanceof Storage) {
            window.localStorage.setItem(key, JSON.stringify(value));
        }
    }
}
