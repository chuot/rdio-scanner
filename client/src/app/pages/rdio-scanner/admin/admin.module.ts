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

import { NgModule } from '@angular/core';
import { RdioScannerAdminModule } from '../../../components/rdio-scanner/admin';
import { AppSharedModule } from '../../../shared/shared.module';
import { RdioScannerAdminPageComponent } from './admin.component';
import { routes } from './admin.routes';

@NgModule({
    declarations: [RdioScannerAdminPageComponent],
    exports: [RdioScannerAdminPageComponent],
    imports: [
        RdioScannerAdminModule,
        AppSharedModule.forChild({routerRoutes: routes }),
    ],
})
export class RdioScannerAdminPageModule { }
