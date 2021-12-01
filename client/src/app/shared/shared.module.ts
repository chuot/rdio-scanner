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

import { CommonModule } from '@angular/common';
import { ModuleWithProviders, NgModule } from '@angular/core';
import { FormsModule, ReactiveFormsModule } from '@angular/forms';
import { ExtraOptions, RouterModule, Routes } from '@angular/router';
import { AppMaterialModule } from './material/material.module';
import { AppUpdateModule } from './update/update.module';

export interface AppSharedModuleConfig {
    routerExtraOptions?: ExtraOptions;
    routerRoutes?: Routes;
}

@NgModule({
    exports: [
        AppMaterialModule,
        AppUpdateModule,
        CommonModule,
        FormsModule,
        ReactiveFormsModule,
        RouterModule,
    ],
})
export class AppSharedModule {
    static forChild(config: AppSharedModuleConfig = {}): ModuleWithProviders<AppSharedModule> {
        return {
            ngModule: AppSharedModule,
            providers: [
                ...RouterModule.forChild(config.routerRoutes || []).providers || [],
            ],
        };
    }

    static forRoot(config: AppSharedModuleConfig = {}): ModuleWithProviders<AppSharedModule> {
        return {
            ngModule: AppSharedModule,
            providers: [
                ...AppUpdateModule.forRoot().providers || [],
                ...RouterModule.forRoot(config.routerRoutes || [], config.routerExtraOptions || {}).providers || [],
            ],
        };
    }
}
