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
    paused = false;
    rx = false;

    private subcriptions: Subscription[] = [];

    constructor(private appRadioService: AppRadioService) { }

    ngOnInit(): void {
        this.subscribe();
    }

    ngOnDestroy(): void {
        this.unsubscribe();
    }

    private handleCallEvent(event: RadioEvent): void {
        if ('call' in event) {
            this.rx = event.call === null ? false : true;
        }
    }

    private handlePauseEvent(event: RadioEvent): void {
        if ('pause' in event) {
            this.paused = event.pause;
        }
    }

    private subscribe(): void {
        this.subcriptions.push(this.appRadioService.event.subscribe((event: RadioEvent) =>  {
            this.handleCallEvent(event);
            this.handlePauseEvent(event);
        }));
    }

    private unsubscribe(subscriptions: Subscription[] = this.subcriptions): void {
        while (subscriptions.length) {
            subscriptions.pop().unsubscribe();
        }
    }
}
