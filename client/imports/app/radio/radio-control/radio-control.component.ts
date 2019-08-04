import { Component, OnDestroy, OnInit } from '@angular/core';
import { Subscription } from 'rxjs';
import { RadioEvent } from '../radio';
import { AppRadioService } from '../radio.service';

@Component({
    selector: 'app-radio-control',
    styleUrls: [
        '../radio.scss',
        './radio-control.component.scss',
    ],
    templateUrl: './radio-control.component.html',
})
export class AppRadioControlComponent implements OnDestroy, OnInit {
    liveFeed: boolean = this.appRadioService.live;
    paused = false;
    systemHold = false;
    talkgroupHold = false;

    private _subscriptions: Subscription[] = [];

    constructor(public appRadioService: AppRadioService) { }

    ngOnInit(): void {
        this._subscribe();
    }

    ngOnDestroy(): void {
        this._unsubscribe();
    }

    avoid(): void {
        this.appRadioService.avoid();
    }

    holdSystem(): void {
        this.appRadioService.holdSystem();
    }

    holdTalkgroup(): void {
        this.appRadioService.holdTalkgroup();
    }

    pause(): void {
        this.appRadioService.pause();
    }

    replayLast(): void {
        this.appRadioService.replay();
    }

    searchCall(): void {
        this.appRadioService.search();
    }

    selectTalkgroups(): void {
        this.appRadioService.select();
    }

    skipNext(): void {
        this.appRadioService.skip(true);
    }

    toggleLiveFeed(): void {
        this.appRadioService.liveFeed();
    }

    private _handlePauseEvent(event: RadioEvent): void {
        if ('pause' in event) {
            this.paused = event.pause;
        }
    }

    private _handleRadioEvent(event: RadioEvent): void {
        if ('hold' in event) {
            switch (event.hold) {
                case '-sys':
                    this.systemHold = false;
                    break;
                case '+sys':
                    this.systemHold = true;
                    break;
                case '-tg':
                    this.talkgroupHold = false;
                    break;
                case '+tg':
                    this.talkgroupHold = true;
                    break;
            }
        } else if ('live' in event) {
            this.liveFeed = event.live;
        }
    }

    private _subscribe(): void {
        this._subscriptions.push(
            this.appRadioService.event.subscribe((event: RadioEvent) => {
                this._handlePauseEvent(event);
                this._handleRadioEvent(event);
            }),
        );
    }

    private _unsubscribe(subscriptions: Subscription[] = this._subscriptions): void {
        while (subscriptions.length) {
            subscriptions.pop().unsubscribe();
        }
    }
}
