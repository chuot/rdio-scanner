import { Injectable } from '@angular/core';
import { Query } from 'apollo-angular';
import gql from 'graphql-tag';
import { RdioScannerSystem, RdioScannerTalkgroup } from './rdio-scanner-systems-query.service';

export interface RdioScannerCall {
    audio?: {
        type?: 'Buffer';
        data?: number[];
    };
    emergency?: boolean;
    freq?: number;
    freqList?: RdioScannerCallFreq[];
    id?: string;
    startTime?: Date;
    stopTime?: Date;
    srcList?: RdioScannerCallSrc[];
    system?: number;
    talkgroup?: number;
    talkgroupData?: RdioScannerTalkgroup;
    systemData?: RdioScannerSystem;
}

export interface RdioScannerCallFreq {
    errorCount?: number;
    freq?: number;
    len?: number;
    pos?: number;
    spikeCount?: number;
}

export interface RdioScannerCallSrc {
    pos?: number;
    src?: number;
}

export interface RdioScannerCallQueryResponse {
    rdioScannerCall: RdioScannerCall;
}

@Injectable({
    providedIn: 'root',
})
export class AppRdioScannerCallQueryService extends Query<RdioScannerCallQueryResponse> {
    document = gql`
        query rdioScannerCall($id: String) {
            rdioScannerCall(id: $id) {
                id
                audio
                emergency
                freq
                freqList {
                    errorCount
                    freq
                    len
                    pos
                    spikeCount
                }
                startTime
                stopTime
                srcList {
                    pos
                    src
                }
                system
                talkgroup
            }
        }
    `;
}
