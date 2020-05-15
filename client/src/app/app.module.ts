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

import { NgModule } from '@angular/core';
import { BrowserModule } from '@angular/platform-browser';
import { BrowserAnimationsModule } from '@angular/platform-browser/animations';
import { ServiceWorkerModule } from '@angular/service-worker';
import { environment } from '../environments/environment';
import { AppComponent } from './app.component';
import { AppRdioScannerModule } from './components/rdio-scanner/rdio-scanner.module';
import { AppSharedModule } from './shared/shared.module';

@NgModule({
    bootstrap: [AppComponent],
    declarations: [AppComponent],
    imports: [
        AppRdioScannerModule,
        AppSharedModule.forRoot(),
        BrowserAnimationsModule,
        BrowserModule,
        ServiceWorkerModule.register('ngsw-worker.js', { enabled: environment.production }),
    ],
})
export class AppModule { }
