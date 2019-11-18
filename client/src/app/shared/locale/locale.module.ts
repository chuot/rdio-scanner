import { registerLocaleData } from '@angular/common';
import locale from '@angular/common/locales/en-CA';
import { LOCALE_ID as NG_LOCALE_ID, ModuleWithProviders, NgModule } from '@angular/core';

const LOCALE_ID = 'en-CA'; // Because we need dates as yyyy-mm-dd

@NgModule()
export class AppLocaleModule {
    static forRoot(): ModuleWithProviders {
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
