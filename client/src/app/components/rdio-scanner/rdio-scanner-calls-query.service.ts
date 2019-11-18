import { Injectable } from '@angular/core';
import { Query } from 'apollo-angular';
import gql from 'graphql-tag';
import { RdioScannerSystem, RdioScannerTalkgroup } from './rdio-scanner-systems-query.service';

export interface RdioScannerCall {
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

export interface RdioScannerCallsQueryResponse {
    rdioScannerCalls: {
        count: number;
        dateStart: string;
        dateStop: string;
        results: RdioScannerCall[];
    };
}

@Injectable({
    providedIn: 'root',
})
export class AppRdioScannerCallsQueryService extends Query<RdioScannerCallsQueryResponse> {
    document = gql`
        query rdioScannerCalls(
            $date: Date
            $first: Int
            $last: Int
            $skip: Int
            $sort: Int
            $system: Int
            $talkgroup: Int
        ) {
            rdioScannerCalls(
                date: $date
                first: $first
                last: $last
                skip: $skip
                sort: $sort
                system: $system
                talkgroup: $talkgroup
            ) {
                count
                dateStart
                dateStop
                results {
                    id
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
        }
    `;
}
