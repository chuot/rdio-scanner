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

import { ChangeDetectorRef, Component, OnDestroy, ViewChild } from '@angular/core';
import { FormBuilder, FormGroup } from '@angular/forms';
import { MatPaginator } from '@angular/material/paginator';
import { BehaviorSubject } from 'rxjs';
import {
    RdioScannerCall,
    RdioScannerConfig,
    RdioScannerEvent,
    RdioScannerLivefeedMode,
    RdioScannerPlaybackList,
    RdioScannerSearchOptions,
    RdioScannerSystem,
    RdioScannerTalkgroup,
} from '../rdio-scanner';
import { RdioScannerService } from '../rdio-scanner.service';

@Component({
    selector: 'rdio-scanner-search',
    styleUrls: ['./search.component.scss'],
    templateUrl: './search.component.html',
    standalone: false
})
export class RdioScannerSearchComponent implements OnDestroy {
    call: RdioScannerCall | undefined;
    callPending: number | undefined;

    form: FormGroup;

    livefeedOnline = false;
    livefeedPlayback = false;

    playbackList: RdioScannerPlaybackList | undefined;

    optionsGroup: string[] = [];
    optionsSystem: string[] = [];
    optionsTag: string[] = [];
    optionsTalkgroup: string[] = [];

    paused = false;

    results = new BehaviorSubject(new Array<RdioScannerCall | null>(10));
    resultsPending = false;

    time12h = false;

    private config: RdioScannerConfig | undefined;

    private eventSubscription;

    private limit = 200;

    private offset = 0;

    @ViewChild(MatPaginator, { read: MatPaginator }) private paginator: MatPaginator | undefined;

    constructor(
        private rdioScannerService: RdioScannerService,
        private ngChangeDetectorRef: ChangeDetectorRef,
        private ngFormBuilder: FormBuilder,
    ) {
        this.form = this.ngFormBuilder.group<{
            date: string | null;
            group: number;
            sort: number;
            system: number;
            tag: number;
            talkgroup: number;
            unit: number;
        }>({
            date: null,
            group: -1,
            sort: -1,
            system: -1,
            tag: -1,
            talkgroup: -1,
            unit: -1,
        });

        this.eventSubscription = this.rdioScannerService.event.subscribe((event: RdioScannerEvent) => this.eventHandler(event));
    }

    download(id: number): void {
        this.rdioScannerService.loadAndDownload(id);
    }

    formChangeHandler(): void {
        if (this.livefeedPlayback) {
            this.rdioScannerService.stopPlaybackMode();
        }

        this.paginator?.firstPage();

        this.refreshFilters();

        this.searchCalls();
    }

    ngOnDestroy(): void {
        this.eventSubscription.unsubscribe();
    }

    play(id: number): void {
        this.rdioScannerService.loadAndPlay(id);
    }

    refreshFilters(): void {
        if (!this.config) {
            return;
        }

        const selectedGroup = this.getSelectedGroup();
        const selectedSystem = this.getSelectedSystem();
        const selectedTag = this.getSelectedTag();
        const selectedTalkgroup = this.getSelectedTalkgroup();

        this.optionsSystem = this.config.systems
            .filter((system) => {
                const group = selectedGroup === undefined ||
                    system.talkgroups.some((talkgroup) => talkgroup.groups.includes(selectedGroup));
                const tag = selectedTag === undefined ||
                    system.talkgroups.some((talkgroup) => talkgroup.tag === selectedTag);
                return group && tag;
            })
            .map((system) => system.label);

        this.optionsTalkgroup = selectedSystem == undefined
            ? []
            : selectedSystem.talkgroups
                .filter((talkgroup) => {
                    const group = selectedGroup == undefined ||
                        talkgroup.groups.includes(selectedGroup);
                    const tag = selectedTag == undefined ||
                        talkgroup.tag === selectedTag;
                    return group && tag;
                })
                .map((talkgroup) => talkgroup.label);

        this.optionsGroup = Object.keys(this.config.groups)
            .filter((group) => {
                const system: boolean = selectedSystem === undefined ||
                    selectedSystem.talkgroups.some((talkgroup) => talkgroup.groups.includes(group))
                const talkgroup: boolean = selectedTalkgroup === undefined ||
                    selectedTalkgroup.groups.includes(group);
                const tag: boolean = selectedTag === undefined ||
                    (selectedTalkgroup !== undefined && selectedTalkgroup.tag === selectedTag) ||
                    (this.config !== undefined && this.config.systems
                        .flatMap((system) => system.talkgroups)
                        .some((talkgroup) => talkgroup.groups.includes(group) && talkgroup.tag === selectedTag))
                return system && talkgroup && tag;
            })
            .sort((a, b) => a.localeCompare(b))

        this.optionsTag = Object.keys(this.config.tags)
            .filter((tag) => {
                const system: boolean = selectedSystem === undefined ||
                    selectedSystem.talkgroups.some((talkgroup) => talkgroup.tag === tag)
                const talkgroup: boolean = selectedTalkgroup === undefined ||
                    selectedTalkgroup.tag === tag;
                const group: boolean = selectedGroup === undefined ||
                    (selectedTalkgroup !== undefined && selectedTalkgroup.groups.includes(selectedGroup)) ||
                    (this.config !== undefined && this.config.systems
                        .flatMap((system) => system.talkgroups)
                        .some((talkgroup) => talkgroup.tag === tag && talkgroup.groups.includes(selectedGroup)))
                return system && talkgroup && group;
            })
            .sort((a, b) => a.localeCompare(b))

        this.form.patchValue({
            group: selectedGroup ? this.optionsGroup.findIndex((group) => group === selectedGroup) : -1,
            system: selectedSystem ? this.optionsSystem.findIndex((system) => system === selectedSystem.label) : -1,
            tag: selectedTag ? this.optionsTag.findIndex((tag) => tag === selectedTag) : -1,
            talkgroup: selectedTalkgroup ? this.optionsTalkgroup.findIndex((talkgroup) => talkgroup === selectedTalkgroup.label) : -1,
        });
    }

    refreshResults(): void {
        if (!this.paginator) {
            return;
        }

        const from = this.paginator.pageIndex * this.paginator.pageSize;

        const to = this.paginator.pageIndex * this.paginator.pageSize + this.paginator.pageSize - 1;

        if (!this.callPending && (from >= this.offset + this.limit || from < this.offset)) {
            this.searchCalls();

        } else if (this.playbackList) {
            const calls: Array<RdioScannerCall | null> = this.playbackList.results.slice(from % this.limit, to % this.limit + 1);

            while (calls.length < this.results.value.length) {
                calls.push(null);
            }

            this.results.next(calls);
        }
    }

    resetForm(): void {
        this.form.reset({
            date: null,
            group: -1,
            sort: -1,
            system: -1,
            tag: -1,
            talkgroup: -1,
            unit: -1,
        });

        this.paginator?.firstPage();

        this.formChangeHandler();
    }

    searchCalls(): void {
        if (this.livefeedPlayback) {
            return;
        }

        const pageIndex = this.paginator?.pageIndex || 0;

        const pageSize = this.paginator?.pageSize || 0;

        this.offset = Math.floor((pageIndex * pageSize) / this.limit) * this.limit;

        const options: RdioScannerSearchOptions = {
            limit: this.limit,
            offset: this.offset,
            sort: this.form.get('sort')?.value ?? -1,
        };

        if (typeof this.form.value.date === 'string') {
            options.date = new Date(Date.parse(this.form.value.date));
        }

        if ((this.form.get('group')?.value ?? -1) >= 0) {
            const group = this.getSelectedGroup();

            if (group) {
                options.group = group;
            }
        }

        if ((this.form.get('system')?.value ?? -1) >= 0) {
            const system = this.getSelectedSystem();

            if (system) {
                options.system = system.id;
            }
        }

        if ((this.form.get('tag')?.value ?? -1) >= 0) {
            const tag = this.getSelectedTag();

            if (tag) {
                options.tag = tag;
            }
        }

        if ((this.form.get('talkgroup')?.value ?? -1) >= 0) {
            const talkgroup = this.getSelectedTalkgroup();

            if (talkgroup) {
                options.talkgroup = talkgroup.id;
            }
        }

        this.resultsPending = true;

        this.form.disable();

        this.rdioScannerService.searchCalls(options);
    }

    stop(): void {
        if (this.livefeedPlayback) {
            this.rdioScannerService.stopPlaybackMode();

        } else {
            this.rdioScannerService.stop();
        }
    }

    private eventHandler(event: RdioScannerEvent): void {
        if ('call' in event) {
            this.call = event.call;

            if (this.callPending) {
                const index = this.results.value.findIndex((call) => call?.id === this.callPending);

                if (index === -1) {
                    if (this.form.get('sort')?.value === -1) {
                        this.paginator?.previousPage();

                    } else {
                        this.paginator?.nextPage();
                    }
                }

                this.callPending = undefined;
            }
        }

        if ('config' in event) {
            this.config = event.config;

            this.callPending = undefined;

            this.optionsGroup = Object.keys(this.config?.groups || []).sort((a, b) => a.localeCompare(b));
            this.optionsSystem = (this.config?.systems || []).map((system) => system.label);
            this.optionsTag = Object.keys(this.config?.tags || []).sort((a, b) => a.localeCompare(b));

            this.time12h = this.config?.time12hFormat || false;
        }

        if ('livefeedMode' in event) {
            this.livefeedOnline = event.livefeedMode === RdioScannerLivefeedMode.Online;

            this.livefeedPlayback = event.livefeedMode === RdioScannerLivefeedMode.Playback;
        }

        if ('playbackList' in event) {
            this.playbackList = event.playbackList;

            this.refreshResults();

            this.resultsPending = false;

            this.form.enable();
        }

        if ('playbackPending' in event) {
            this.callPending = event.playbackPending;
        }

        if ('pause' in event) {
            this.paused = event.pause || false;
        }

        this.ngChangeDetectorRef.detectChanges();
    }

    private getSelectedGroup(): string | undefined {
        return this.optionsGroup[this.form.get('group')?.value ?? -1];
    }

    private getSelectedSystem(): RdioScannerSystem | undefined {
        return this.config?.systems.find((system) => system.label === this.optionsSystem[this.form.get('system')?.value ?? -1]);
    }

    private getSelectedTag(): string | undefined {
        return this.optionsTag[this.form.get('tag')?.value ?? -1];
    }

    private getSelectedTalkgroup(): RdioScannerTalkgroup | undefined {
        const system = this.getSelectedSystem();

        return system
            ? system.talkgroups.find((talkgroup) => talkgroup.label === this.optionsTalkgroup[this.form.get('talkgroup')?.value ?? -1])
            : undefined;
    }
}
