import { Call } from '../../../../imports/models/call';
import { System as RadioSystem } from '../../../../imports/models/system';
import { Talkgroup as RadioTalkgroup } from '../../../../imports/models/talkgroup';

export { Calls as RadioCalls} from '../../../../imports/collections/calls';
export { Systems as RadioSystems} from '../../../../imports/collections/systems';
export { CallFreq as RadioCallFreq, CallSrc as RadioCallSrc } from '../../../../imports/models/call';
export { System as RadioSystem } from '../../../../imports/models/system';
export { Talkgroup as RadioTalkgroup } from '../../../../imports/models/talkgroup';

export interface RadioAvoids {
    [key: number]: {
        [key: number]: boolean;
    };
}

export interface RadioCall extends Call {
    alphaTag?: string;
    description?: string;
    mode?: string;
    tag?: string;
    group?: string;
    systemData?: RadioSystem;
    talkgroupData?: RadioTalkgroup;
}

export interface RadioEvent {
    avoid?: { sys: number, tg: number, status: boolean };
    avoids?: RadioAvoids;
    call?: RadioCall | null;
    hold?: '-sys' | '+sys' | '-tg' | '+tg';
    live?: boolean;
    pause?: boolean;
    queue?: number;
    search?: null;
    select?: null;
    systems?: RadioSystem[];
    time?: number;
}
