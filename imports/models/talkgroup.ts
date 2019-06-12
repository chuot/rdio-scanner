export class Talkgroup {
    alphaTag: string;
    dec: number;
    description: string;
    group: string;
    hex: string;
    mode: string;
    tag: string;

    constructor(data: any = {}) {
        this.alphaTag = typeof data.alphaTag === 'string' ? data.alphaTag : '';
        this.dec = typeof data.dec === 'number' ? data.dec : parseInt(data.dec, 10) || 0;
        this.description = typeof data.description === 'string' ? data.description : '';
        this.group = typeof data.group === 'string' ? data.group : '';
        this.hex = typeof data.hex === 'string' ? data.hex : '';
        this.mode = typeof data.mode === 'string' ? data.mode : '';
        this.tag = typeof data.tag === 'string' ? data.tag : '';
    }
}
