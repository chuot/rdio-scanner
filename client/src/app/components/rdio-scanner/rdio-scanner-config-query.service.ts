import { Injectable } from '@angular/core';
import { Query } from 'apollo-angular';
import gql from 'graphql-tag';

export interface RdioScannerConfig {
    allowDownload?: boolean;
    useGroup?: boolean;
}

export interface RdioScannerConfigQueryResponse {
    rdioScannerConfig: RdioScannerConfig;
}

@Injectable({
    providedIn: 'root',
})
export class AppRdioScannerConfigQueryService extends Query<RdioScannerConfigQueryResponse> {
    document = gql`
        query rdioScannerConfig {
            rdioScannerConfig {
                allowDownload
                useGroup
            }
        }
    `;
}
