/*
 * *****************************************************************************
 * Copyright (C) 2019-2026 Chrystian Huot <chrystian.huot@saubeo.solutions>
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

import { HttpClient, HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { EventEmitter, Injectable, OnDestroy } from '@angular/core';
import { AbstractControl, FormBuilder, FormGroup, ValidationErrors, ValidatorFn, Validators } from '@angular/forms';
import { MatSnackBar } from '@angular/material/snack-bar';
import { firstValueFrom, timer } from 'rxjs';
import { AppUpdateService } from '../../../shared/update/update.service';

export interface Access {
    _id?: string;
    code?: string;
    expiration?: Date;
    ident?: string;
    limit?: number;
    order?: number;
    systems?: {
        id: number;
        talkgroups: {
            id: number;
        }[] | number[] | '*';
    }[] | number[] | '*';
}

export interface AdminEvent {
    authenticated?: boolean;
    config?: Config;
    docker?: boolean;
    passwordNeedChange?: boolean;
}

export interface ApiKey {
    _id?: string;
    disabled?: boolean;
    ident?: string;
    key?: string;
    order?: number;
    systems?: {
        id: number;
        talkgroups: number[] | '*';
    }[] | number[] | '*';
}

export interface Config {
    access?: Access[];
    apiKeys?: ApiKey[];
    dirWatch?: DirWatch[];
    downstreams?: Downstream[];
    groups?: Group[];
    options?: Options;
    systems?: System[];
    tags?: Tag[];
}

export interface DirWatch {
    _id?: string;
    delay?: number;
    deleteAfter?: boolean;
    directory?: string;
    disabled?: boolean;
    extension?: string;
    frequency?: number;
    mask?: string;
    order?: number;
    systemId?: number;
    talkgroupId?: number;
    type?: string;
}

export interface Downstream {
    _id?: string;
    apiKey?: string;
    disabled?: boolean;
    order?: number;
    systems?: {
        id?: number;
        id_as?: number;
        talkgroups?: {
            id: number;
            id_as?: number;
        }[] | number[] | '*';
    }[] | number[] | '*';
    url?: string;
}

export interface Group {
    _id?: number;
    label?: string;
}

export interface Log {
    _id: number;
    dateTime: Date;
    level: number;
    message: string;
}

export interface LogsQuery {
    count: number;
    dateStart: Date;
    dateStop: Date;
    options: LogsQueryOptions;
    logs: Log[];
}

export interface LogsQueryOptions {
    date?: Date;
    level?: 'error' | 'info' | 'warn';
    limit: number;
    offset: number;
    sort: number;
}

export interface Options {
    afsSystems?: string;
    audioConversion?: 0 | 1 | 2 | 3;
    autoPopulate?: boolean;
    branding?: string;
    dimmerDelay?: number;
    disableDuplicateDetection?: boolean;
    duplicateDetectionTimeFrame?: number;
    email?: string;
    keypadBeeps?: string;
    maxClients?: number;
    playbackGoesLive?: boolean;
    pruneDays?: number;
    searchPatchedTalkgroups?: boolean;
    showListenersCount?: boolean;
    sortTalkgroups?: boolean;
    tagsToggle?: boolean;
    time12hFormat?: boolean;
}

export interface System {
    _id?: number;
    autoPopulate?: boolean;
    blacklists?: string;
    id?: number;
    label?: string;
    led?: string | null;
    order?: number | null;
    talkgroups?: Talkgroup[];
    units?: Unit[];
}

export interface Tag {
    _id?: number;
    label?: string;
}

export interface Talkgroup {
    frequency?: number | null;
    groupId?: number;
    id?: number;
    label?: string;
    led?: string | null;
    name?: string;
    order?: number;
    tagId?: number;
}

export interface Unit {
    id?: number | null;
    label?: string;
    order?: number;
}

enum url {
    config = 'config',
    login = 'login',
    logout = 'logout',
    logs = 'logs',
    password = 'password',
}

const SESSION_STORAGE_KEY = 'rdio-scanner-admin-token';

@Injectable()
export class RdioScannerAdminService implements OnDestroy {
    event = new EventEmitter<AdminEvent>();

    get authenticated() {
        return !!this.token;
    }

    get docker() {
        return this._docker;
    }

    get passwordNeedChange() {
        return this._passwordNeedChange;
    }

    private configWebSocket: WebSocket | undefined;

    private _docker = false;

    private _passwordNeedChange = false;

    private get token(): string {
        return window?.sessionStorage?.getItem(SESSION_STORAGE_KEY) || '';
    }

    private set token(token: string) {
        window?.sessionStorage?.setItem(SESSION_STORAGE_KEY, token);
    }

    constructor(
        appUpdateService: AppUpdateService,
        private matSnackBar: MatSnackBar,
        private ngFormBuilder: FormBuilder,
        private ngHttpClient: HttpClient,
    ) {
        this.configWebSocketOpen();
    }

    ngOnDestroy(): void {
        this.event.complete();

        this.configWebSocketClose();
    }

    async changePassword(currentPassword: string, newPassword: string): Promise<void> {
        try {
            const res = await firstValueFrom(this.ngHttpClient.post<{ passwordNeedChange: boolean }>(
                this.getUrl(url.password),
                { currentPassword, newPassword },
                { headers: this.getHeaders(), responseType: 'json' },
            ));

            this._passwordNeedChange = res.passwordNeedChange;

            this.event.next({ passwordNeedChange: this.passwordNeedChange });

        } catch (error) {
            this.errorHandler(error);

            throw error;

        }
    }

    async getConfig(): Promise<Config> {
        try {
            const res = await firstValueFrom(this.ngHttpClient.get<{
                config: Config;
                docker: boolean;
                passwordNeedChange: boolean;
            }>(
                this.getUrl(url.config),
                { headers: this.getHeaders(), responseType: 'json' },
            ));

            if (res.docker !== this._docker) {
                this._docker = res.docker;

                this.event.emit({ docker: this.docker })
            }

            if (res.passwordNeedChange !== this._passwordNeedChange) {
                this._passwordNeedChange = res.passwordNeedChange;

                this.event.emit({ passwordNeedChange: this.passwordNeedChange });
            }

            return res.config;

        } catch (error) {
            this.errorHandler(error);
        }

        return {};
    }

    getLeds(): string[] {
        return ['blue', 'cyan', 'green', 'magenta', 'orange', 'red', 'white', 'yellow'];
    }

    async getLogs(options: LogsQueryOptions): Promise<LogsQuery | undefined> {
        try {
            const res = await firstValueFrom(this.ngHttpClient.post<LogsQuery>(
                this.getUrl(url.logs),
                options,
                { headers: this.getHeaders(), responseType: 'json' },
            ));

            return res;

        } catch (error) {
            this.errorHandler(error);

            return undefined;
        }
    }

    async login(password: string): Promise<boolean> {
        try {
            const res = await firstValueFrom(this.ngHttpClient.post<{
                passwordNeedChange: boolean,
                token: string
            }>(
                this.getUrl(url.login),
                { password },
                { headers: this.getHeaders(), responseType: 'json' },
            ));

            this.token = res.token;

            this._passwordNeedChange = res.passwordNeedChange;

            this.event.emit({
                authenticated: this.authenticated,
                passwordNeedChange: res.passwordNeedChange,
            });

            this.configWebSocketOpen();

            return !!this.token;

        } catch (error) {
            this.errorHandler(error);

            return false;
        }
    }

    async logout(): Promise<boolean> {
        try {
            this.ngHttpClient.post(
                this.getUrl(url.logout),
                null,
                { headers: this.getHeaders(), responseType: 'text' },
            );

            this.configWebSocketClose();

            this.token = '';

            this.event.emit({ authenticated: this.authenticated });

            return true;

        } catch (error) {
            this.errorHandler(error);

            return false;
        }
    }

    async saveConfig(config: Config): Promise<Config> {
        try {
            const res = await firstValueFrom(this.ngHttpClient.put<{ config: Config }>(
                this.getUrl(url.config),
                config,
                { headers: this.getHeaders(), responseType: 'json' },
            ));

            return res.config;

        } catch (error) {
            this.errorHandler(error);

            return config;
        }
    }

    newAccessForm(access?: Access): FormGroup {
        return this.ngFormBuilder.group({
            _id: [access?._id],
            code: [access?.code, [Validators.required, this.validateAccessCode()]],
            expiration: [access?.expiration],
            ident: [access?.ident, Validators.required],
            limit: [access?.limit],
            order: [access?.order],
            systems: [access?.systems, Validators.required],
        });
    }

    newApiKeyForm(apiKey?: ApiKey): FormGroup {
        return this.ngFormBuilder.group({
            _id: [apiKey?._id],
            disabled: [apiKey?.disabled],
            ident: [apiKey?.ident, Validators.required],
            key: [apiKey?.key, [Validators.required, this.validateApiKey()]],
            order: [apiKey?.order],
            systems: [apiKey?.systems, Validators.required],
        });
    }

    newConfigForm(config?: Config): FormGroup {
        return this.ngFormBuilder.group({
            access: this.ngFormBuilder.array(config?.access?.map((access) => this.newAccessForm(access)) || []),
            apiKeys: this.ngFormBuilder.array(config?.apiKeys?.map((apiKey) => this.newApiKeyForm(apiKey)) || []),
            dirWatch: this.ngFormBuilder.array(config?.dirWatch?.map((dirWatch) => this.newDirWatchForm(dirWatch)) || []),
            downstreams: this.ngFormBuilder.array(config?.downstreams?.map((downstream) => this.newDownstreamForm(downstream)) || []),
            groups: this.ngFormBuilder.array(config?.groups?.map((group) => this.newGroupForm(group)) || []),
            options: this.newOptionsForm(config?.options),
            systems: this.ngFormBuilder.array(config?.systems?.map((system) => this.newSystemForm(system)) || []),
            tags: this.ngFormBuilder.array(config?.tags?.map((tag) => this.newTagForm(tag)) || []),
        });
    }

    newDirWatchForm(dirWatch?: DirWatch): FormGroup {
        return this.ngFormBuilder.group({
            _id: [dirWatch?._id],
            delay: [typeof dirWatch?.delay === 'number' ? Math.max(2000, dirWatch?.delay) : 2000],
            deleteAfter: [dirWatch?.deleteAfter],
            directory: [dirWatch?.directory, [Validators.required, this.validateDirectory()]],
            disabled: [dirWatch?.disabled],
            extension: [dirWatch?.extension, this.validateExtension()],
            frequency: [dirWatch?.frequency, Validators.min(0)],
            mask: [dirWatch?.mask, this.validateMask()],
            order: [dirWatch?.order],
            systemId: [dirWatch?.systemId, this.validateDirwatchSystemId()],
            talkgroupId: [dirWatch?.talkgroupId, this.validateDirwatchTalkgroupId()],
            type: [dirWatch?.type],
        });
    }

    newDownstreamForm(downstream?: Downstream): FormGroup {
        return this.ngFormBuilder.group({
            _id: [downstream?._id],
            apiKey: [downstream?.apiKey, [Validators.required, this.validateApiKey()]],
            disabled: [downstream?.disabled],
            order: [downstream?.order],
            systems: [downstream?.systems, Validators.required],
            url: [downstream?.url, [Validators.required, this.validateUrl(), this.validateDownstreamUrl()]],
        });
    }

    newGroupForm(group?: Group): FormGroup {
        return this.ngFormBuilder.group({
            _id: [group?._id],
            label: [group?.label, Validators.required],
        });
    }

    newTagForm(tag?: Tag): FormGroup {
        return this.ngFormBuilder.group({
            _id: [tag?._id],
            label: [tag?.label, Validators.required],
        });
    }

    newSystemForm(system?: System): FormGroup {
        return this.ngFormBuilder.group({
            _id: [system?._id],
            autoPopulate: [system?.autoPopulate],
            blacklists: [system?.blacklists, this.validateBlacklists()],
            id: [system?.id, [Validators.required, Validators.min(1), this.validateId()]],
            label: [system?.label, Validators.required],
            led: [system?.led],
            order: [system?.order],
            talkgroups: this.ngFormBuilder.array(system?.talkgroups?.map((talkgroup) => this.newTalkgroupForm(talkgroup)) || []),
            units: this.ngFormBuilder.array(system?.units?.map((unit) => this.newUnitForm(unit)) || []),
        });
    }

    newTalkgroupForm(talkgroup?: Talkgroup): FormGroup {
        return this.ngFormBuilder.group({
            frequency: [talkgroup?.frequency, Validators.min(0)],
            groupId: [talkgroup?.groupId, [Validators.required, this.validateGroup()]],
            id: [talkgroup?.id, [Validators.required, Validators.min(1), this.validateId()]],
            label: [talkgroup?.label, Validators.required],
            led: [talkgroup?.led],
            name: [talkgroup?.name, Validators.required],
            order: [talkgroup?.order],
            tagId: [talkgroup?.tagId, [Validators.required, this.validateTag()]],
        });
    }

    newUnitForm(unit?: Unit): FormGroup {
        return this.ngFormBuilder.group({
            id: [unit?.id, [Validators.required, Validators.min(0), this.validateId()]],
            label: [unit?.label, Validators.required],
            order: [unit?.order],
        });
    }

    newOptionsForm(options?: Options): FormGroup {
        return this.ngFormBuilder.group({
            afsSystems: [options?.afsSystems, this.validateAfsSystems()],
            audioConversion: [options?.audioConversion],
            autoPopulate: [options?.autoPopulate],
            branding: [options?.branding],
            dimmerDelay: [options?.dimmerDelay, [Validators.required, Validators.min(0)]],
            disableDuplicateDetection: [options?.disableDuplicateDetection],
            duplicateDetectionTimeFrame: [options?.duplicateDetectionTimeFrame, [Validators.required, Validators.min(0)]],
            email: [options?.email],
            keypadBeeps: [options?.keypadBeeps, Validators.required],
            maxClients: [options?.maxClients, [Validators.required, Validators.min(1)]],
            playbackGoesLive: [options?.playbackGoesLive],
            pruneDays: [options?.pruneDays, [Validators.required, Validators.min(0)]],
			searchPatchedTalkgroups: [options?.searchPatchedTalkgroups],
			showListenersCount: [options?.showListenersCount],
            sortTalkgroups: [options?.sortTalkgroups],
            tagsToggle: [options?.tagsToggle],
            time12hFormat: [options?.time12hFormat],
        });
    }

    private configWebSocketClose(): void {
        if (this.configWebSocket instanceof WebSocket) {
            this.configWebSocket.onclose = null;
            this.configWebSocket.onmessage = null;
            this.configWebSocket.onopen = null;

            this.configWebSocket.close();

            this.configWebSocket = undefined;
        }
    }

    private configWebSocketReconnect(): void {
        this.configWebSocketClose();

        this.configWebSocketOpen();
    }

    private configWebSocketOpen(): void {
        if (!this.token) {
            return;
        }

        const webSocketUrl = new URL(this.getUrl(url.config), window.location.href).href.replace(/^http/, 'ws');

        this.configWebSocket = new WebSocket(webSocketUrl);

        this.configWebSocket.onclose = (ev: CloseEvent) => {
            if (ev.code === 1000) {
                this.token = '';

                this.event.emit({ authenticated: this.authenticated });
            } else {
                timer(2000).subscribe(() => this.configWebSocketReconnect());
            }
        };

        this.configWebSocket.onopen = () => {
            this.configWebSocket?.send(this.token);

            if (this.configWebSocket instanceof WebSocket) {
                this.configWebSocket.onmessage = (ev: MessageEvent<string>) => {
                    this.event.emit({ config: JSON.parse(ev.data) });
                }
            }
        }
    }

    private errorHandler(error: unknown): void {
        if (!(error instanceof HttpErrorResponse)) {
            return;
        }

        if (error.status === 401) {
            this.token = '';

            this.event.emit({ authenticated: this.authenticated });

            this.configWebSocketClose();

        } else {
            this.matSnackBar.open(error.message, '', { duration: 5000 });
        }
    }

    private getHeaders(): HttpHeaders {
        return new HttpHeaders({
            Authorization: this.token || '',
        });
    }

    private getUrl(path: string): string {
        return `${window.location.href}/../api/admin${path.charAt(0) === '/' ? path : `/${path}`}`;
    }

    private validateAccessCode(): ValidatorFn {
        return (control: AbstractControl): ValidationErrors | null => {
            if (typeof control.value !== 'string' || !control.value.length) {
                return null;
            }

            const access: Access[] = control.parent?.parent?.getRawValue() || [];

            const count = access.reduce((c, a) => c += a.code === control.value ? 1 : 0, 0);

            return count > 1 ? { duplicate: true } : null;
        };
    }

    private validateAfsSystems(): ValidatorFn {
        return (control: AbstractControl): ValidationErrors | null => {
            return typeof control.value === 'string' && control.value.length ? /^[0-9]+(,[0-9]+)*$/.test(control.value) ? null : { invalid: true } : null;
        };
    }

    private validateApiKey(): ValidatorFn {
        return (control: AbstractControl): ValidationErrors | null => {
            if (typeof control.value !== 'string' || !control.value.length) {
                return null;
            }

            const apiKeys: ApiKey[] = control.parent?.parent?.getRawValue() || [];

            const count = apiKeys.reduce((c, a) => c += a.key === control.value ? 1 : 0, 0);

            return count > 1 ? { duplicate: true } : null;
        };
    }

    private validateBlacklists(): ValidatorFn {
        return (control: AbstractControl): ValidationErrors | null => {
            return typeof control.value === 'string' && control.value.length ? /^[0-9]+(,[0-9]+)*$/.test(control.value) ? null : { invalid: true } : null;
        };
    }

    private validateDirectory(): ValidatorFn {
        return (control: AbstractControl): ValidationErrors | null => {
            if (typeof control.value !== 'string' || !control.value.length) {
                return null;
            }

            if (control.value.startsWith('\\')) {
                return { network: true }
            }

            const dirWatch: DirWatch[] = control.parent?.parent?.getRawValue() || [];

            const count = dirWatch.reduce((c, a) => c += a.directory === control.value ? 1 : 0, 0);

            return count > 1 ? { duplicate: true } : null;
        };
    }

    private validateDirwatchSystemId(): ValidatorFn {
        return (control: AbstractControl): ValidationErrors | null => {
            const dirwatch = control.parent?.getRawValue() || {};

            const mask = dirwatch.mask || '';

            const type = dirwatch.type;

            return ['dsdplus', 'trunk-recorder', 'sdr-trunk'].includes(type) || control.value !== null || /#SYS/.test(mask) ? null : { required: true };
        };
    }

    private validateDirwatchTalkgroupId(): ValidatorFn {
        return (control: AbstractControl): ValidationErrors | null => {
            const dirwatch = control.parent?.getRawValue() || {};

            const mask = dirwatch.mask || '';

            const type = dirwatch.type;

            return ['dsdplus', 'trunk-recorder', 'sdr-trunk'].includes(type) || control.value !== null || /#TG/.test(mask) ? null : { required: true };
        };
    }

    private validateDownstreamUrl(): ValidatorFn {
        return (control: AbstractControl): ValidationErrors | null => {
            if (typeof control.value !== 'string' || !control.value.length) {
                return null;
            }

            const downstream: Downstream[] = control.parent?.parent?.getRawValue() || [];

            const count = downstream.reduce((c, a) => c += a.url === control.value ? 1 : 0, 0);

            return count > 1 ? { duplicate: true } : null;
        };
    }

    private validateExtension(): ValidatorFn {
        return (control: AbstractControl): ValidationErrors | null => {
            if (typeof control.value !== 'string' || !control.value.length) {
                return null;
            }

            return /^[0-9a-zA-Z]+$/.test(control.value) ? null : { invalid: true };
        };
    }

    private validateGroup(): ValidatorFn {
        return (control: AbstractControl): ValidationErrors | null => {
            if (typeof control.value !== 'number') {
                return null;
            }

            const groupIds = control.root.get('groups')?.value.map((group: Group) => group._id);

            return groupIds ? groupIds.includes(control.value) ? null : { required: true } : null;
        };
    }

    private validateId(): ValidatorFn {
        return (control: AbstractControl): ValidationErrors | null => {
            if (control.value === null || typeof control.value !== 'number') {
                return null;
            }

            const systems: System[] = control.parent?.parent?.getRawValue() || [];

            const count = systems.reduce((c, s) => c += s.id === control.value ? 1 : 0, 0);

            return count > 1 ? { duplicate: true } : null;
        };
    }

    private validateMask(): ValidatorFn {
        return (control: AbstractControl): ValidationErrors | null => {
            if (typeof control.value !== 'string') {
                return null;
            }

            const masks = ['#DATE', '#GROUP', '#HZ', '#KHZ', '#MHZ', '#SYS', '#SYSLBL', '#TAG', '#TG', '#TGAFS', '#TGHZ', '#TGKHZ', '#TGLBL', '#TGMHZ', '#TIME', '#UNIT', '#ZTIME'];

            const metas = control.value.match(/(#[A-Z]+)/g) || [];

            const count = metas.reduce((c, m) => {
                if (masks.includes(m)) {
                    c++;
                }

                return c;
            }, 0);

            return count ? null : { invalid: true };
        };
    }

    private validateTag(): ValidatorFn {
        return (control: AbstractControl): ValidationErrors | null => {
            if (typeof control.value !== 'number') {
                return null;
            }

            const tagIds = control.root.get('tags')?.value.map((tag: Tag) => tag._id);

            return tagIds ? tagIds.includes(control.value) ? null : { required: true } : null;
        };
    }

    private validateUrl(): ValidatorFn {
        return (control: AbstractControl): ValidationErrors | null => {
            if (typeof control.value !== 'string' || !control.value.length) {
                return null;
            }

            return /^https?:\/\/.+$/.test(control.value) ? null : { invalid: true }
        };
    }
}
