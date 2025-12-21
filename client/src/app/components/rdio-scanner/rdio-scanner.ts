/*
 * *****************************************************************************
 * Copyright (C) 2019-2026 Chrystian Huot <chrystian@huot.qc.ca>
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

import { Subscription } from "rxjs";

export interface RdioScannerAvoidOptions {
    all?: boolean;
    call?: RdioScannerCall;
    minutes?: number;
    status?: boolean;
    system?: RdioScannerSystem;
    talkgroup?: RdioScannerTalkgroup;
}

export interface RdioScannerAlerts {
    [key: string]: RdioScannerOscillatorData[];
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
    delayed: boolean;
    frequencies?: RdioScannerCallFrequency[];
    frequency?: number;
    groupsData?: RdioScannerGroupData[];
    id: number;
    patches: number[];
    source?: number;
    sources?: RdioScannerCallSource[];
    system: number;
    tagData?: RdioScannerTagData;
    talkgroup: number;
    talkgroupData?: RdioScannerTalkgroup;
    systemData?: RdioScannerSystem;
}

export interface RdioScannerCallFrequency {
    dbm?: number;
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
    alerts?: RdioScannerAlerts;
    branding?: string;
    dimmerDelay: number | false;
    email?: string;
    groups: { [key: string]: { [key: number]: number[] } };
    groupsData: RdioScannerGroupData[];
    keypadBeeps: RdioScannerKeypadBeeps | undefined;
    playbackGoesLive: boolean;
    showListenersCount: boolean;
    systems: RdioScannerSystem[];
    tags: { [key: string]: { [key: number]: number[] } };
    tagsData: RdioScannerTagData[];
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

export interface RdioScannerGroupData {
    id: number;
    alert?: string;
    label?: string;
    led?: string;
}

export interface RdioScannerKeypadBeeps {
    [RdioScannerBeepStyle.Activate]: RdioScannerOscillatorData[];
    [RdioScannerBeepStyle.Deactivate]: RdioScannerOscillatorData[];
    [RdioScannerBeepStyle.Denied]: RdioScannerOscillatorData[];
}

export interface RdioScannerLivefeed {
    active: boolean;
    minutes: number | undefined;
    timer: Subscription | undefined;
}

export interface RdioScannerLivefeedMap {
    [key: number]: {
        [key: number]: RdioScannerLivefeed;
    };
}

export enum RdioScannerLivefeedMode {
    Offline = 'offline',
    Online = 'online',
    Playback = 'playback',
}

export interface RdioScannerOscillatorData {
    begin: number;
    end: number;
    frequency: number;
    type: OscillatorType;
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
    unit?: number;
}

export interface RdioScannerSystem {
    id: number;
    alert?: string;
    label: string;
    led?: 'blue' | 'cyan' | 'green' | 'magenta' | 'orange' | 'red' | 'white' | 'yellow';
    order?: number;
    talkgroups: RdioScannerTalkgroup[];
    type?: string;
    units: RdioScannerUnit[];
}

export interface RdioScannerTagData {
    id: number;
    alert?: string;
    label?: string;
    led?: string;
}

export interface RdioScannerTalkgroup {
    alert?: string;
    frequency?: number;
    groups: string[];
    id: number;
    label: string;
    led?: 'blue' | 'cyan' | 'green' | 'magenta' | 'orange' | 'red' | 'white' | 'yellow';
    name: string;
    tag: string;
    type?: string;
}

export interface RdioScannerUnit {
    id: number;
    label: string;
    unitRef: number;
    unitFrom: number;
    unitTo: number;
}
