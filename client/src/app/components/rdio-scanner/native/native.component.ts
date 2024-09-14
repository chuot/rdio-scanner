/*
 * *****************************************************************************
 * Copyright (C) 2019-2024 Chrystian Huot <chrystian@huot.qc.ca>
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

import { Component, Optional, OnInit } from '@angular/core';
import { MatSnackBarRef } from '@angular/material/snack-bar';
import { timer } from 'rxjs';

@Component({
    selector: 'RdioScannerNative',
    styleUrls: ['./native.component.scss'],
    templateUrl: './native.component.html',
})
export class RdioScannerNativeComponent implements OnInit {
    countdown = 10;

    deviceType = 'mobile';

    isAndroid = false;
    isApple = false;

    constructor(@Optional() private matSnackBarRef: MatSnackBarRef<RdioScannerNativeComponent>) { }

    ngOnInit(): void {
        const ua = navigator.userAgent;

        if (ua.includes('Android')) {
            this.deviceType = 'Android';
            this.isAndroid = true;
            this.wait();

        } else if (ua.includes('iPad') || ua.includes('iPhone')) {
            this.deviceType = 'Apple';
            this.isApple = true;
            this.wait();

        } else {
            this.matSnackBarRef?.dismiss();
        }
    }

    private wait(): void {
        timer(1000).subscribe(() => {
            this.countdown--;

            if (this.countdown < 1) {
                this.matSnackBarRef?.dismiss();

            } else {
                this.wait();
            }
        });
    }
}