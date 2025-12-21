/*
 * *****************************************************************************
 * Copyright (C) 2019-2026 Chrystian Huot <chrystian@huot.qc.ca>
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

import { Component, OnDestroy, ViewEncapsulation } from '@angular/core';
import { AdminEvent, RdioScannerAdminService, Group, Tag } from './admin.service';

@Component({
    encapsulation: ViewEncapsulation.None,
    selector: 'rdio-scanner-admin',
    styleUrls: ['./admin.component.scss'],
    templateUrl: './admin.component.html',
    standalone: false
})
export class RdioScannerAdminComponent implements OnDestroy {
    authenticated = true;

    groups: Group[] = [];

    tags: Tag[] = [];

    private eventSubscription;

    constructor(private adminService: RdioScannerAdminService) {
        this.eventSubscription = this.adminService.event.subscribe(async (event: AdminEvent) => {
            if ('authenticated' in event) {
                this.authenticated = event.authenticated || false;
            }
        });
    }

    ngOnDestroy(): void {
        this.eventSubscription.unsubscribe();
    }

    async logout(): Promise<void> {
        await this.adminService.logout();
    }
}
