import { NgModule } from '@angular/core';
import { BrowserModule } from '@angular/platform-browser';
import { BrowserAnimationsModule } from '@angular/platform-browser/animations';
import { RouterModule } from '@angular/router';
import { AppComponent } from './app.component';
import { AppSharedModule } from './shared/shared.module';

@NgModule({
    bootstrap: [AppComponent],
    declarations: [AppComponent],
    imports: [
        AppSharedModule,
        BrowserAnimationsModule,
        BrowserModule,
        RouterModule.forRoot([
            {
                // loadChildren: () => import('./radio/radio.module').then((m) => m.AppRadioModule),
                loadChildren: './radio/radio.module#AppRadioModule',
                path: '',
            },
            {
                path: '**',
                pathMatch: 'full',
                redirectTo: '',
            },
        ]),
    ],
})
export class AppModule { }
