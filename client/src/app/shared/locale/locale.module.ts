/*
 * *****************************************************************************
 * Copyright (C) 2019-2020 Chrystian Huot
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

import { registerLocaleData } from '@angular/common';
import locale from '@angular/common/locales/en-CA';
import { LOCALE_ID as NG_LOCALE_ID, ModuleWithProviders, NgModule } from '@angular/core';

const LOCALE_ID = 'en-CA'; // Because we need dates as yyyy-mm-dd

@NgModule()
export class AppLocaleModule {
    static forRoot(): ModuleWithProviders<AppLocaleModule> {
        return {
            ngModule: AppLocaleModule,
            providers: [
                { provide: NG_LOCALE_ID, useValue: LOCALE_ID },
            ],
        };
    }

    constructor() {
        registerLocaleData(locale, LOCALE_ID);
    }
}
