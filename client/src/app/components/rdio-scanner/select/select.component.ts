/*
 * *****************************************************************************
 * Copyright (C) 2019-2021 Chrystian Huot <chrystian.huot@saubeo.solutions>
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
import { RdioScannerService } from '../rdio-scanner.service';

@Component({
    selector: 'rdio-scanner-select',
    styleUrls: [
        '../common.scss',
        './select.component.scss',
    ],
    templateUrl: './select.component.html',
})
export class RdioScannerSelectComponent implements OnDestroy {
    groups: RdioScannerGroup[] | undefined;

    map: RdioScannerLivefeedMap = {};

    systems: RdioScannerSystem[] | undefined;

    private eventSubscription = this.rdioScannerService.event.subscribe((event: RdioScannerEvent) => this.eventHandler(event));

    constructor(private rdioScannerService: RdioScannerService) { }

    avoid(options?: RdioScannerAvoidOptions): void {
        this.rdioScannerService.beep(RdioScannerBeepStyle.Activate);

        this.rdioScannerService.avoid(options);
    }

    ngOnDestroy(): void {
        this.eventSubscription.unsubscribe();
    }

    toggleGroup(label: string): void {
        this.rdioScannerService.beep();

        this.rdioScannerService.toggleGroup(label);
    }

    private eventHandler(event: RdioScannerEvent): void {
        if (event.config) {
            this.systems = event.config.systems;
        }

        if (event.groups) {
            this.groups = event.groups;
        }

        if (event.map) {
            this.map = event.map;
        }
    }
}
