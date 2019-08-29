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

    private subscriptions: Subscription[] = [];

    constructor(private appRadioService: AppRadioService) { }

    ngOnInit(): void {
        this.subscribe();
    }

    ngOnDestroy(): void {
        this.unsubscribe();
    }

    @HostListener('window:beforeunload', ['$event'])
    private exitNotification(event: BeforeUnloadEvent): void {
        if (this.appRadioService.live) {
            event.preventDefault();
            event.returnValue = false;
        }
    }

    private subscribe(): void {
        this.subscriptions.push(
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

    private unsubscribe(subscriptions: Subscription[] = this.subscriptions): void {
        while (subscriptions.length) {
            subscriptions.pop().unsubscribe();
        }
    }
}
