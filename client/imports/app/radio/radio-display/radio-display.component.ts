import { Component, OnDestroy, OnInit } from '@angular/core';
import { Subscription } from 'rxjs';
import { RadioCall, RadioCallFreq, RadioCallSrc, RadioEvent } from '../radio';
import { AppRadioService } from '../radio.service';

@Component({
    selector: 'app-radio-display',
    styleUrls: [
        '../radio.scss',
        './radio-display.component.scss',
    ],
    templateUrl: './radio-display.component.html',
})
export class AppRadioDisplayComponent implements OnDestroy, OnInit {
    clock = new Date();
    currentCall: RadioCall;
    currentError = 0;
    currentFrequency = 0;
    currentSpike = 0;
    currentTime: Date;
    currentUnit = 0;
    history = new Array<RadioCall>(5);
    live = false;
    queueLength = 0;

    private _clockInterval: NodeJS.Timeout;
    private _subscriptions: Subscription[] = [];

    constructor(public appRadioService: AppRadioService) { }

    ngOnInit(): void {
        this._clockInterval = setInterval(() => this.clock = new Date(), 1000);

        this._subscribe();
    }

    ngOnDestroy(): void {
        this._unsubscribe();

        clearInterval(this._clockInterval);
    }

    private _handleRadioCallEvent(event: RadioEvent): void {
        if (
            'call' in event &&
            event.call !== null &&
            event.call !== this.currentCall &&
            !this.history.find((call) => call === event.call)
        ) {
            this.history.pop();
            this.history.unshift(this.currentCall);
            this.currentCall = event.call;
            this.currentTime = event.call.startTime;
        }
    }

    private _handleRadioLiveEvent(event: RadioEvent): void {
        if ('live' in event) {
            this.live = event.live;
        }
    }

    private _handleRadioQueueEvent(event: RadioEvent): void {
        if ('queue' in event) {
            this.queueLength = event.queue;
        }
    }

    private _handleRadioTimeEvent(event: RadioEvent): void {
        if ('time' in event) {
            const currentFreq = this.currentCall.freqList
                .reduce((cur: RadioCallFreq, freq: RadioCallFreq) => freq.pos < event.time ? freq : cur, null);

            const currentSrc = this.currentCall.srcList
                .reduce((cur: RadioCallSrc, src: RadioCallSrc) => src.pos < event.time ? src : cur, null);

            const currentError = currentFreq && currentFreq.errorCount;
            const currentFrequency = (currentFreq && currentFreq.freq);
            const currentSpike = currentFreq && currentFreq.spikeCount;

            const currentUnit = currentSrc && currentSrc.src;

            this.currentError = typeof currentError === 'number' ? currentError : 0;
            this.currentFrequency = typeof currentFrequency === 'number' ? currentFrequency : this.currentCall.freq;
            this.currentSpike = typeof currentSpike === 'number' ? currentSpike : 0;
            this.currentUnit = typeof currentUnit === 'number' ? currentUnit : 0;

            if (this.currentCall && this.currentCall.startTime instanceof Date) {
                this.currentTime = new Date(
                    this.currentCall.startTime.getFullYear(),
                    this.currentCall.startTime.getMonth(),
                    this.currentCall.startTime.getDate(),
                    this.currentCall.startTime.getHours(),
                    this.currentCall.startTime.getMinutes(),
                    this.currentCall.startTime.getSeconds() + event.time
                );
            }
        }
    }

    private _subscribe(): void {
        this._subscriptions.push(
            this.appRadioService.event.subscribe((event: RadioEvent) => {
                this._handleRadioCallEvent(event);
                this._handleRadioLiveEvent(event);
                this._handleRadioQueueEvent(event);
                this._handleRadioTimeEvent(event);
            }),
        );
    }

    private _unsubscribe(subscriptions: Subscription[] = this._subscriptions): void {
        while (subscriptions.length) {
            subscriptions.pop().unsubscribe();
        }
    }
}
