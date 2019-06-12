import { Component, OnDestroy, OnInit } from '@angular/core';
import { Subscription } from 'rxjs';
import { RadioEvent } from '../radio';
import { AppRadioService } from '../radio.service';

@Component({
    selector: 'app-radio-led',
    styleUrls: [
        '../radio.scss',
        './radio-led.component.scss',
    ],
    templateUrl: './radio-led.component.html',
})
export class AppRadioLedComponent implements OnDestroy, OnInit {
    rx = false;

    private _subcriptions: Subscription[] = [];

    constructor(public appRadioService: AppRadioService) { }

    ngOnInit(): void {
        this._subscribe();
    }

    ngOnDestroy(): void {
        this._unsubscribe();
    }

    private _handleCallEvent(event: RadioEvent): void {
        if ('call' in event) {
            this.rx = event.call === null ? false : true;
        }
    }

    private _subscribe(): void {
        this._subcriptions.push(this.appRadioService.event.subscribe((event: RadioEvent) => this._handleCallEvent(event)));
    }

    private _unsubscribe(subscriptions: Subscription[] = this._subcriptions): void {
        while (subscriptions.length) {
            subscriptions.pop().unsubscribe();
        }
    }
}
