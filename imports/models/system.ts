import { BaseModel } from './base-model';
import { Talkgroup } from './talkgroup';

export class System extends BaseModel {
    name: string;
    system: number;
    talkgroups: Talkgroup[];

    constructor(data: any = {}) {
        super();

        this.name = typeof data.name === 'string' ? data.name : '';
        this.system = typeof data.system === 'number' ? data.system : parseInt(data.system, 10) || 0;
        this.talkgroups = Array.isArray(data.talkgroups) ? data.talkgroups.map((talkgroup: Talkgroup) => {
            return new Talkgroup(talkgroup);
        }) : [];
    }
}
