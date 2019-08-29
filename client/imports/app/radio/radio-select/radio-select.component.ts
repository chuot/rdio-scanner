import { Component, OnDestroy, OnInit } from '@angular/core';
import { Subscription } from 'rxjs';
import { RadioAvoids, RadioEvent, RadioSystem, RadioTalkgroup } from '../radio';
import { AppRadioService } from '../radio.service';

@Component({
    selector: 'app-radio-select',
    styleUrls: [
        '../radio.scss',
        './radio-select.component.scss',
    ],
    templateUrl: './radio-select.component.html',
})
export class AppRadioSelectComponent implements OnDestroy, OnInit {
    avoids: RadioAvoids;

    systems: RadioSystem[] = [];

    private subscriptions: Subscription[] = [];

    constructor(private appRadioService: AppRadioService) {
        this.avoids = this.appRadioService.getAvoids();
    }

    ngOnInit(): void {
        this.subscribe();
    }

    ngOnDestroy(): void {
        this.unsubscribe();
    }

    avoid(system: RadioSystem | null, talkgroup: RadioTalkgroup | null, status: boolean): void {
        if (system) {
            this.appRadioService.avoid({ system, talkgroup, status });
        } else {
            this.appRadioService.avoid({ all: true, status });
        }
    }

    close(): void {
        this.appRadioService.select();
    }

    private handleAvoidEvent(event: RadioEvent): void {
        if ('avoid' in event) {
            this.avoids[event.avoid.sys][event.avoid.tg] = event.avoid.status;
        }
    }

    private handleAvoidsEvent(event: RadioEvent): void {
        if ('avoids' in event) {
            this.avoids = event.avoids;
        }
    }

    private handleSystemsEvent(event: RadioEvent): void {
        if ('systems' in event) {
            this.systems.splice(0, this.systems.length, ...event.systems);
        }
    }

    private subscribe(): void {
        this.subscriptions.push(
            this.appRadioService.event.subscribe((event: RadioEvent) => {
                this.handleAvoidEvent(event);
                this.handleSystemsEvent(event);
            })
        );
    }

    private unsubscribe(subscriptions: Subscription[] = this.subscriptions) {
        while (subscriptions.length) {
            subscriptions.pop().unsubscribe();
        }
    }
}
