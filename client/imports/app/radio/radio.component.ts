import { Component, HostListener, OnDestroy, OnInit } from '@angular/core';
import { Subscription } from 'rxjs';
import { RadioEvent } from './radio';
import { AppRadioService } from './radio.service';

@Component({
    selector: 'app-radio',
    styleUrls: [
        './radio.scss',
        './radio.component.scss',
    ],
    templateUrl: './radio.component.html',
})
export class AppRadioComponent implements OnDestroy, OnInit {
    searchPanelOpened = false;
    selectPanelOpened = false;

    private _subscriptions: Subscription[] = [];

    constructor(public appRadioService: AppRadioService) { }

    ngOnInit(): void {
        this._subscribe();
    }

    ngOnDestroy(): void {
        this._unsubscribe();
    }

    @HostListener('window:beforeunload', ['$event'])
    private _exitNotification(event: BeforeUnloadEvent): void {
        if (this.appRadioService.live) {
            event.preventDefault();
            event.returnValue = false;
        }
    }

    private _subscribe(): void {
        this._subscriptions.push(
            this.appRadioService.event.subscribe((event: RadioEvent) => {
                if ('search' in event) {
                    this.searchPanelOpened = !this.searchPanelOpened;
                    this.selectPanelOpened = false;
                }
                if ('select' in event) {
                    this.selectPanelOpened = !this.selectPanelOpened;
                    this.searchPanelOpened = false;
                }
            }),
        );
    }

    private _unsubscribe(subscriptions: Subscription[] = this._subscriptions): void {
        while (subscriptions.length) {
            subscriptions.pop().unsubscribe();
        }
    }
}
