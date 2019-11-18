import { NgModule } from '@angular/core';
import { BrowserModule } from '@angular/platform-browser';
import { BrowserAnimationsModule } from '@angular/platform-browser/animations';
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
    ],
})
export class AppModule { }
