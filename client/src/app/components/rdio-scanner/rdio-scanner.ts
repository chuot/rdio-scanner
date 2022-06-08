/*
 * *****************************************************************************
 * Copyright (C) 2019-2022 Chrystian Huot <chrystian.huot@saubeo.solutions>
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

export interface RdioScannerBeep {
    begin: number;
    end: number;
    frequency: number;
    type: OscillatorType;
}

export enum RdioScannerBeepStyle {
    Activate = 'activate',
    Deactivate = 'deactivate',
    Denied = 'denied',
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
    id: number;
    patches: number[];
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

export interface RdioScannerCategory {
    label: string;
    status: RdioScannerCategoryStatus;
    type: RdioScannerCategoryType;
}

export enum RdioScannerCategoryStatus {
    Off = 'off',
    On = 'on',
    Partial = 'partial',
}

export enum RdioScannerCategoryType {
    Group = 'group',
    Tag = 'tag',
}

export interface RdioScannerConfig {
    afs?: string;
    dimmerDelay: number | false;
    groups: { [key: string]: { [key: number]: number[] } };
    keypadBeeps: RdioScannerKeypadBeeps | false;
    playbackGoesLive: boolean;
    showListenersCount: boolean;
    systems: RdioScannerSystem[];
    tags: { [key: string]: { [key: number]: number[] } };
    tagsToggle: boolean;
    time12hFormat: boolean;
}

export interface RdioScannerEvent {
    auth?: boolean;
    categories?: RdioScannerCategory[];
    call?: RdioScannerCall;
    config?: RdioScannerConfig;
    expired?: boolean;
    holdSys?: boolean;
    holdTg?: boolean;
    linked?: boolean;
    listeners?: number;
    livefeedMode?: RdioScannerLivefeedMode;
    map?: RdioScannerLivefeedMap;
    pause?: boolean;
    playbackList?: RdioScannerPlaybackList;
    playbackPending?: number;
    queue?: number;
    time?: number;
    tooMany?: boolean;
}

export interface RdioScannerKeypadBeeps {
    [RdioScannerBeepStyle.Activate]: RdioScannerBeep[];
    [RdioScannerBeepStyle.Deactivate]: RdioScannerBeep[];
    [RdioScannerBeepStyle.Denied]: RdioScannerBeep[];
}

export interface RdioScannerLivefeedMap {
    [key: string]: {
        [key: string]: boolean;
    };
}

export enum RdioScannerLivefeedMode {
    Offline = 'offline',
    Online = 'online',
    Playback = 'playback',
}

export interface RdioScannerPlaybackList {
    count: number;
    dateStart: Date;
    dateStop: Date;
    options: RdioScannerSearchOptions;
    results: RdioScannerCall[];
}

export interface RdioScannerSearchOptions {
    date?: Date;
    group?: string;
    limit: number;
    offset: number;
    sort: number;
    system?: number;
    tag?: string;
    talkgroup?: number;
}

export interface RdioScannerSystem {
    id: number;
    label: string;
    led?: 'blue' | 'cyan' | 'green' | 'magenta' | 'orange' | 'red' | 'white' | 'yellow';
    order?: number;
    talkgroups: RdioScannerTalkgroup[];
    units: RdioScannerUnit[];
}

export interface RdioScannerTalkgroup {
    frequency?: number;
    group: string;
    id: number;
    label: string;
    led?: 'blue' | 'cyan' | 'green' | 'magenta' | 'orange' | 'red' | 'white' | 'yellow';
    name: string;
    tag: string;
}

export interface RdioScannerUnit {
    id: number;
    label: string;
}
