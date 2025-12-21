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

import { provideHttpClient, withInterceptorsFromDi } from '@angular/common/http';
import { NgModule } from '@angular/core';
import { AppSharedModule } from '../../../shared/shared.module';
import { RdioScannerAdminComponent } from './admin.component';
import { RdioScannerAdminService } from './admin.service';
import { RdioScannerAdminConfigComponent } from './config/config.component';
import { RdioScannerAdminAccessComponent } from './config/access/access.component';
import { RdioScannerAdminApikeysComponent } from './config/apikeys/apikeys.component';
import { RdioScannerAdminDirwatchComponent } from './config/dirwatch/dirwatch.component';
import { RdioScannerAdminDownstreamsComponent } from './config/downstreams/downstreams.component';
import { RdioScannerAdminGroupsComponent } from './config/groups/groups.component';
import { RdioScannerAdminOptionsComponent } from './config/options/options.component';
import { RdioScannerAdminSiteComponent } from './config/systems/site/site.component';
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
import { RdioScannerAdminImportTalkgroupsComponent } from './tools/import-talkgroups/import-talkgroups.component';
import { RdioScannerAdminImportUnitsComponent } from './tools/import-units/import-units.component';
import { RdioScannerAdminPasswordComponent } from './tools/password/password.component';

@NgModule({ declarations: [
        RdioScannerAdminComponent,
        RdioScannerAdminConfigComponent,
        RdioScannerAdminAccessComponent,
        RdioScannerAdminApikeysComponent,
        RdioScannerAdminDirwatchComponent,
        RdioScannerAdminDownstreamsComponent,
        RdioScannerAdminGroupsComponent,
        RdioScannerAdminImportExportConfigComponent,
        RdioScannerAdminImportTalkgroupsComponent,
        RdioScannerAdminImportUnitsComponent,
        RdioScannerAdminLoginComponent,
        RdioScannerAdminLogsComponent,
        RdioScannerAdminOptionsComponent,
        RdioScannerAdminPasswordComponent,
        RdioScannerAdminSiteComponent,
        RdioScannerAdminSystemComponent,
        RdioScannerAdminSystemsComponent,
        RdioScannerAdminSystemsSelectComponent,
        RdioScannerAdminTagsComponent,
        RdioScannerAdminTalkgroupComponent,
        RdioScannerAdminTodosComponent,
        RdioScannerAdminToolsComponent,
        RdioScannerAdminUnitComponent,
    ],
    exports: [RdioScannerAdminComponent], imports: [AppSharedModule], providers: [RdioScannerAdminService, provideHttpClient(withInterceptorsFromDi())] })
export class RdioScannerAdminModule { }
