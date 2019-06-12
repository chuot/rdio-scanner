import { NgModule } from '@angular/core';
import { RouterModule } from '@angular/router';
import { AppSharedModule } from '../shared/shared.module';
import { AppRadioControlComponent } from './radio-control/radio-control.component';
import { AppRadioDisplayComponent } from './radio-display/radio-display.component';
import { AppRadioFrequencyPipe } from './radio-frequency.pipe';
import { AppRadioLedComponent } from './radio-led/radio-led.component';
import { AppRadioSearchComponent } from './radio-search/radio-search.component';
import { AppRadioSelectComponent } from './radio-select/radio-select.component';
import { AppRadioComponent } from './radio.component';
import { AppRadioService } from './radio.service';

@NgModule({
    declarations: [
        AppRadioComponent,
        AppRadioControlComponent,
        AppRadioDisplayComponent,
        AppRadioFrequencyPipe,
        AppRadioLedComponent,
        AppRadioSearchComponent,
        AppRadioSelectComponent,
    ],
    exports: [AppRadioComponent],
    imports: [
        AppSharedModule,
        RouterModule.forChild([
            {
                component: AppRadioComponent,
                path: '',
            }
        ])
    ],
    providers: [AppRadioService],
})
export class AppRadioModule { }
