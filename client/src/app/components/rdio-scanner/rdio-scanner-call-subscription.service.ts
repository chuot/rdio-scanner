import { Injectable } from '@angular/core';
import { Subscription } from 'apollo-angular';
import gql from 'graphql-tag';
import { RdioScannerSystem, RdioScannerTalkgroup } from './rdio-scanner-systems-subscription.service';

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

export interface RdioScannerCallSubscriptionResponse {
    rdioScannerCall: RdioScannerCall;
}

@Injectable({
    providedIn: 'root',
})
export class AppRdioScannerCallSubscriptionService extends Subscription<RdioScannerCallSubscriptionResponse> {
    document = gql`
        subscription rdioScannerCall($selection: String) {
            rdioScannerCall(selection: $selection) {
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
