import { BaseModel } from './base-model';

export interface CallFreq {
    errorCount: number;
    freq: number;
    len: number;
    pos: number;
    spikeCount: number;
    time: Date;
}

export interface CallSrc {
    pos: number;
    src: number;
    time: Date;
}

export class Call extends BaseModel {
    _id?: string;
    audio: string;
    emergency: boolean;
    freq: number;
    freqList: CallFreq[];
    startTime: Date;
    stopTime: Date;
    srcList: CallSrc[];
    system: number;
    talkgroup: number;

    constructor(data: any = {}) {
        super();

        this.audio = parseString(data.audio);
        this.emergency = parseBoolean(data.emergency);
        this.freq = parseNumber(data.freq);
        this.freqList = Array.isArray(data.freqList) ? data.freqList.map((freq: any = {}) => ({
            errorCount: parseNumber(freq.error_count),
            freq: parseNumber(freq.freq),
            len: parseNumber(freq.len),
            pos: parseNumber(freq.pos),
            spikeCount: parseNumber(freq.spike_count),
            time: parseDate(freq.time),
        })) : [];
        this.startTime = parseDate(data.start_time);
        this.stopTime = parseDate(data.stop_time);
        this.srcList = Array.isArray(data.srcList) ? data.srcList.map((src: any = {}) => ({
            pos: parseNumber(src.pos),
            src: parseNumber(src.src),
            time: parseDate(src.time),
        })) : [];
        this.system = parseNumber(data.system);
        this.talkgroup = parseNumber(data.talkgroup);
    }
}

function parseBoolean(value: any): boolean {
    return !!value;
}

function parseDate(value: any): Date {
    if (value instanceof Date) {
        return value;
    } else if (typeof value === 'number') {
        const date = new Date(1970, 0, 1);
        date.setUTCSeconds(value - date.getTimezoneOffset() * 60);
        return date;
    } else {
        return new Date(value);
    }
}

function parseNumber(value: any): number {
    return typeof value === 'number' ? value : parseInt(value, 10) || 0;
};

function parseString(value: any): string {
    return typeof value === 'string' ? value : '';
}
