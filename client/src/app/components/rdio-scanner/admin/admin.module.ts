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

import { HttpClientModule } from '@angular/common/http';
import { NgModule } from '@angular/core';
import { AppSharedModule } from '../../../shared/shared.module';
import { RdioScannerAdminComponent } from './admin.component';
import { RdioScannerAdminService } from './admin.service';
import { RdioScannerAdminConfigComponent } from './config/config.component';
import { RdioScannerAdminAccessComponent } from './config/access/access.component';
import { RdioScannerAdminApiKeysComponent } from './config/api-keys/api-keys.component';
import { RdioScannerAdminDirWatchComponent } from './config/dir-watch/dir-watch.component';
import { RdioScannerAdminDownstreamsComponent } from './config/downstreams/downstreams.component';
import { RdioScannerAdminGroupsComponent } from './config/groups/groups.component';
import { RdioScannerAdminOptionsComponent } from './config/options/options.component';
import { RdioScannerAdminSystemsSelectComponent } from './config/systems/select/select.component';
import { RdioScannerAdminSystemComponent } from './config/systems/system/system.component';
import { RdioScannerAdminSystemsComponent } from './config/systems/systems.component';
import { RdioScannerAdminTalkgroupComponent } from './config/systems/talkgroup/talkgroup.component';
import { RdioScannerAdminUnitComponent } from './config/systems/unit/unit.component';
import { RdioScannerAdminTagsComponent } from './config/tags/tags.component';
import { RdioScannerAdminLoginComponent } from './login/login.component';
import { RdioScannerAdminLogsComponent } from './logs/logs.component';
import { RdioScannerAdminTodosComponent } from './todos/todos.component';
import { RdioScannerAdminToolsComponent } from './tools/tools.component';
import { RdioScannerAdminImportExportConfigComponent } from './tools/import-export-config/import-export-config.component';
import { RdioScannerAdminImportCsvComponent } from './tools/import-csv/import-csv.component';
import { RdioScannerAdminPasswordComponent } from './tools/password/password.component';

@NgModule({
    declarations: [
        RdioScannerAdminComponent,
        RdioScannerAdminConfigComponent,
        RdioScannerAdminAccessComponent,
        RdioScannerAdminApiKeysComponent,
        RdioScannerAdminDirWatchComponent,
        RdioScannerAdminDownstreamsComponent,
        RdioScannerAdminGroupsComponent,
        RdioScannerAdminImportExportConfigComponent,
        RdioScannerAdminImportCsvComponent,
        RdioScannerAdminLoginComponent,
        RdioScannerAdminLogsComponent,
        RdioScannerAdminOptionsComponent,
        RdioScannerAdminPasswordComponent,
        RdioScannerAdminSystemComponent,
        RdioScannerAdminSystemsComponent,
        RdioScannerAdminSystemsSelectComponent,
        RdioScannerAdminTagsComponent,
        RdioScannerAdminTalkgroupComponent,
        RdioScannerAdminTodosComponent,
        RdioScannerAdminToolsComponent,
        RdioScannerAdminUnitComponent,
    ],
    entryComponents: [RdioScannerAdminSystemsSelectComponent],
    exports: [RdioScannerAdminComponent],
    imports: [AppSharedModule, HttpClientModule],
    providers: [RdioScannerAdminService],
})
export class RdioScannerAdminModule { }
