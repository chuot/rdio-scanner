import { NgModule } from '@angular/core';
import { AppSharedModule } from '../../shared/shared.module';
import { AppRdioScannerCallQueryService } from './rdio-scanner-call-query.service';
import { AppRdioScannerCallSubscriptionService } from './rdio-scanner-call-subscription.service';
import { AppRdioScannerCallsQueryService } from './rdio-scanner-calls-query.service';
import { AppRdioScannerSystemsQueryService } from './rdio-scanner-systems-query.service';
import { AppRdioScannerSystemsSubscriptionService } from './rdio-scanner-systems-subscription.service';
import { AppRdioScannerComponent } from './rdio-scanner.component';

@NgModule({
    declarations: [AppRdioScannerComponent],
    exports: [AppRdioScannerComponent],
    imports: [AppSharedModule],
    providers: [
        AppRdioScannerCallQueryService,
        AppRdioScannerCallsQueryService,
        AppRdioScannerCallSubscriptionService,
        AppRdioScannerSystemsQueryService,
        AppRdioScannerSystemsSubscriptionService,
    ],
})
export class AppRdioScannerModule { }
