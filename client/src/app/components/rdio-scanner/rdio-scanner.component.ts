/*
 * *****************************************************************************
 * Copyright (C) 2019-2022 Chrystian Huot <chrystian.huot@saubeo.solutions>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>
 * ****************************************************************************
 */

import { Component, ElementRef, HostListener, OnDestroy, OnInit, ViewChild } from '@angular/core';
import { MatSidenav } from '@angular/material/sidenav';
import { MatSnackBar } from '@angular/material/snack-bar';
import { timer } from 'rxjs';
import { RdioScannerEvent, RdioScannerLivefeedMode } from './rdio-scanner';
import { RdioScannerService } from './rdio-scanner.service';
import { RdioScannerNativeComponent } from './native/native.component';

@Component({
    selector: 'rdio-scanner',
    styleUrls: ['./rdio-scanner.component.scss'],
    templateUrl: './rdio-scanner.component.html',
})
export class RdioScannerComponent implements OnDestroy, OnInit {
    private eventSubscription = this.rdioScannerService.event.subscribe((event: RdioScannerEvent) => this.eventHandler(event));

    private livefeedMode: RdioScannerLivefeedMode = RdioScannerLivefeedMode.Offline;

    @ViewChild('searchPanel') private searchPanel: MatSidenav | undefined;

    @ViewChild('selectPanel') private selectPanel: MatSidenav | undefined;

    constructor(
        private matSnackBar: MatSnackBar,
        private ngElementRef: ElementRef,
        private rdioScannerService: RdioScannerService,
    ) { }

    @HostListener('window:beforeunload', ['$event'])
    exitNotification(event: BeforeUnloadEvent): void {
        if (this.livefeedMode !== RdioScannerLivefeedMode.Offline) {
            event.preventDefault();

            event.returnValue = 'Live Feed is ON, do you really want to leave?';
        }
    }

    start(): void {
        this.rdioScannerService.startLivefeed();
    }

    stop(): void {
        this.rdioScannerService.stopLivefeed();

        this.searchPanel?.close();
        this.selectPanel?.close();
    }

    ngOnDestroy(): void {
        this.eventSubscription.unsubscribe();
    }

    ngOnInit(): void {
        /*
         * BEGIN OF RED TAPE:
         * 
         * By modifying, deleting or disabling the following lines, you harm
         * the open source project and its author.  Rdio Scanner represents a lot of
         * investment in time, support, testing and hardware.
         * 
         * Be respectful, sponsor the project if you can, use native apps when possible.
         * 
         */
        timer(10000).subscribe(() => {
            const ua: String = navigator.userAgent;

            if (ua.includes('Android') || ua.includes('iPad') || ua.includes('iPhone')) {
                this.matSnackBar.openFromComponent(RdioScannerNativeComponent, { panelClass: 'snackbar-white' });
            }
        });
        /**
         * END OF RED TAPE.
         */
    }

    toggleFullscreen(): void {
        if (document.fullscreenElement) {
            const el: {
                exitFullscreen?: () => void;
                mozCancelFullScreen?: () => void;
                msExitFullscreen?: () => void;
                webkitExitFullscreen?: () => void;
            } = document;

            if (el.exitFullscreen) {
                el.exitFullscreen();

            } else if (el.mozCancelFullScreen) {
                el.mozCancelFullScreen();

            } else if (el.msExitFullscreen) {
                el.msExitFullscreen();

            } else if (el.webkitExitFullscreen) {
                el.webkitExitFullscreen();
            }

        } else {
            const el = this.ngElementRef.nativeElement;

            if (el.requestFullscreen) {
                el.requestFullscreen();

            } else if (el.mozRequestFullScreen) {
                el.mozRequestFullScreen();

            } else if (el.msRequestFullscreen) {
                el.msRequestFullscreen();

            } else if (el.webkitRequestFullscreen) {
                el.webkitRequestFullscreen();
            }
        }
    }

    private eventHandler(event: RdioScannerEvent): void {
        if (event.livefeedMode) {
            this.livefeedMode = event.livefeedMode;
        }
    }
}
