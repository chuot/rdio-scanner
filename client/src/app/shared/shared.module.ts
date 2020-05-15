/*
 * *****************************************************************************
 *  Copyright (C) 2019-2020 Chrystian Huot
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
import { FlexLayoutModule } from '@angular/flex-layout';
import { FormsModule, ReactiveFormsModule } from '@angular/forms';
import { AppLocaleModule } from './locale/locale.module';
import { AppMaterialModule } from './material/material.module';

@NgModule({
    exports: [
        AppLocaleModule,
        AppMaterialModule,
        CommonModule,
        FlexLayoutModule,
        FormsModule,
        ReactiveFormsModule,
    ],
})
export class AppSharedModule {
    static forRoot(): ModuleWithProviders {
        return {
            ngModule: AppSharedModule,
            providers: [
                ...AppLocaleModule.forRoot().providers,
            ],
        };
    }
}
