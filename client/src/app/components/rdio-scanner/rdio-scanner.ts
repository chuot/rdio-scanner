/*
 * *****************************************************************************
 * Copyright (C) 2019-2020 Chrystian Huot
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

export interface RdioScannerAvoidOptions {
    all?: boolean;
    call?: RdioScannerCall;
    system?: RdioScannerSystem;
    talkgroup?: RdioScannerTalkgroup;
    status?: boolean;
}

export interface RdioScannerCall {
    audio?: {
        type: 'Buffer';
        data: number[];
    };
    audioName?: string;
    audioType?: string;
    dateTime: Date;
    frequencies?: RdioScannerCallFrequency[];
    frequency?: number;
    id: string;
    source?: number;
    sources?: RdioScannerCallSource[];
    system: number;
    talkgroup: number;
    talkgroupData?: RdioScannerTalkgroup;
    systemData?: RdioScannerSystem;
}

export interface RdioScannerCallFrequency {
    errorCount?: number;
    freq?: number;
    len?: number;
    pos?: number;
    spikeCount?: number;
}

export interface RdioScannerCallSource {
    pos?: number;
    src?: number;
}

export interface RdioScannerConfig {
    allowDownload: boolean;
    systems: RdioScannerSystem[];
    useDimmer: boolean;
    useGroup: boolean;
    useLed: boolean;
}

export interface RdioScannerEvent {
    auth?: boolean;
    config?: RdioScannerConfig;
    call?: RdioScannerCall;
    groups?: RdioScannerGroup[];
    holdSys?: boolean;
    holdTg?: boolean;
    list?: RdioScannerList;
    liveFeed?: boolean;
    map?: RdioScannerLiveFeedMap;
    pause?: boolean;
    queue?: number;
    time?: number;
}

export enum RdioScannerGroupStatus {
    Off = 'off',
    On = 'on',
    Partial = 'partial',
}

export interface RdioScannerGroup {
    label: string;
    status: RdioScannerGroupStatus;
}

export interface RdioScannerList {
    count: number;
    dateStart: Date;
    dateStop: Date;
    results: RdioScannerCall[];
}

export interface RdioScannerLiveFeedMap {
    [key: string]: {
        [key: string]: boolean;
    };
}

export interface RdioScannerSearchOptions {
    date?: Date;
    limit?: number;
    offset?: number;
    sort?: number;
    system?: number;
    talkgroup?: number;
}

export interface RdioScannerSystem {
    id: number;
    label: string;
    led?: 'blue' | 'cyan' | 'green' | 'magenta' | 'red' | 'white' | 'yellow';
    talkgroups: RdioScannerTalkgroup[];
    units?: RdioScannerUnit[];
}

export interface RdioScannerTalkgroup {
    frequency?: number;
    group: string;
    id: number;
    label: string;
    led?: 'blue' | 'cyan' | 'green' | 'magenta' | 'red' | 'white' | 'yellow';
    name: string;
    tag: string;
}

export interface RdioScannerUnit {
    id: number;
    label: string;
}
