/*
 * *****************************************************************************
 * Copyright (C) 2019-2021 Chrystian Huot
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

import { Component, OnDestroy } from '@angular/core';
import { RdioScannerAvoidOptions, RdioScannerBeepStyle, RdioScannerEvent, RdioScannerGroup, RdioScannerLivefeedMap, RdioScannerSystem } from '../rdio-scanner';
import { AppRdioScannerService } from '../rdio-scanner.service';

@Component({
    selector: 'app-rdio-scanner-select',
    styleUrls: [
        '../common.scss',
        './select.component.scss',
    ],
    templateUrl: './select.component.html',
})
export class AppRdioScannerSelectComponent implements OnDestroy {
    groups: RdioScannerGroup[] | undefined;

    map: RdioScannerLivefeedMap = {};

    systems: RdioScannerSystem[] | undefined;

    private eventSubscription = this.appRdioScannerService.event.subscribe((event: RdioScannerEvent) => this.eventHandler(event));

    constructor(private appRdioScannerService: AppRdioScannerService) { }

    avoid(options?: RdioScannerAvoidOptions): void {
        this.appRdioScannerService.beep(RdioScannerBeepStyle.Activate);

        this.appRdioScannerService.avoid(options);
    }

    ngOnDestroy(): void {
        this.eventSubscription.unsubscribe();
    }

    toggleGroup(label: string): void {
        this.appRdioScannerService.beep();

        this.appRdioScannerService.toggleGroup(label);
    }

    private eventHandler(event: RdioScannerEvent): void {
        if (event.config) {
            this.systems = this.sortSystems(event.config.systems);
        }

        if (event.groups) {
            this.groups = event.groups;
        }

        if (event.map) {
            this.map = event.map;
        }
    }

    private sortSystems(systems: RdioScannerSystem[]): RdioScannerSystem[] {
        return systems.sort((sysA, sysB) => {
            if (typeof sysA.order === 'number' && typeof sysB.order !== 'number') {
                return -1;

            } else if (typeof sysA.order !== 'number' && typeof sysB.order === 'number') {
                return 1;

            } else if (typeof sysA.order === 'number' && typeof sysB.order === 'number') {
                return sysA.order - sysB.order;

            } else {
                return sysA.id - sysB.id;
            }
        });
    }
}
