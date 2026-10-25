import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
  maskHasToken,
  normalizeDirwatchRow,
  validDirectory,
  validExtension,
  validateDirwatchRow,
} from '../domain/dirwatch.js';

const validRow = () => ({
    delay: 2000,
    deleteAfter: true,
    directory: '/var/rdio/ingest',
    disabled: false,
    extension: 'mp3',
    frequency: 119100000,
    mask: '#TG_#DATE_#TIME_#HZ',
    order: 1,
    systemId: 11,
    talkgroupId: 1457,
    type: 'default',
});

describe('domain/dirwatch — validateDirwatchRow', () => {
    it('accepts a complete valid row', () => {
        assert.deepEqual(validateDirwatchRow(validRow(), [validRow()]), {});
    });

    it('requires a directory', () => {
        const row = { ...validRow(), directory: '' };

        assert.deepEqual(validateDirwatchRow(row, [row]), { directory: 'Directory is required' });
    });

    it('rejects a directory duplicated across rows', () => {
        const other = { ...validRow(), order: 2 };

        assert.deepEqual(validateDirwatchRow(other, [validRow(), other]), { directory: 'Directory is already defined' });
    });

    it('rejects a network folder starting with a backslash', () => {
        const row = { ...validRow(), directory: '\\\\server\\share' };

        assert.deepEqual(validateDirwatchRow(row, [row]), { directory: 'Incompatible network folder' });
    });

    it('rejects an extension that is not alphanumeric', () => {
        const row = { ...validRow(), extension: 'mp3 ' };

        assert.deepEqual(validateDirwatchRow(row, [row]), { extension: 'Invalid extension' });
    });

    it('accepts an empty extension', () => {
        const row = { ...validRow(), extension: '' };

        assert.deepEqual(validateDirwatchRow(row, [row]), {});
    });

    it('rejects a mask without any known token', () => {
        const row = { ...validRow(), mask: 'cymx_#BOGUS' };

        assert.deepEqual(validateDirwatchRow(row, [row]), { mask: 'Invalid mask' });
    });

    it('rejects an empty mask string', () => {
        const row = { ...validRow(), mask: '' };

        assert.deepEqual(validateDirwatchRow(row, [row]), { mask: 'Invalid mask' });
    });

    it('accepts a mask holding a known token', () => {
        const row = { ...validRow(), mask: 'cymx_#TG_#DATE_#TIME_#HZ' };

        assert.deepEqual(validateDirwatchRow(row, [row]), {});
    });

    it('accepts an absent mask (legacy: only string values are validated)', () => {
        const row = { ...validRow(), type: 'dsdplus', mask: undefined };

        assert.deepEqual(validateDirwatchRow(row, [row]), {});
    });

    it('rejects a delay below 2000 milliseconds', () => {
        const row = { ...validRow(), delay: 1999 };

        assert.deepEqual(validateDirwatchRow(row, [row]), { delay: 'Delay value cannot be less than 2000 milliseconds.' });
    });

    it('accepts a delay of exactly 2000 milliseconds', () => {
        const row = { ...validRow(), delay: 2000 };

        assert.deepEqual(validateDirwatchRow(row, [row]), {});
    });

    it('rejects a frequency below 1', () => {
        const row = { ...validRow(), frequency: 0 };

        assert.deepEqual(validateDirwatchRow(row, [row]), { frequency: 'Invalid frequency' });
    });

    it('accepts an absent frequency', () => {
        const row = { ...validRow(), frequency: undefined };

        assert.deepEqual(validateDirwatchRow(row, [row]), {});
    });

    it('requires a system for a default dirwatch without #SYS', () => {
        const row = { ...validRow(), systemId: null };

        assert.deepEqual(validateDirwatchRow(row, [row]), { systemId: 'System is required' });
    });

    it('exempts the system when the mask contains #SYS', () => {
        const row = { ...validRow(), systemId: null, mask: '#SYS_#TG_#DATE' };

        assert.deepEqual(validateDirwatchRow(row, [row]), {});
    });

    it('exempts the system when the mask contains #SYSLBL (legacy substring match)', () => {
        const row = { ...validRow(), systemId: null, mask: '#SYSLBL_#TG' };

        assert.deepEqual(validateDirwatchRow(row, [row]), {});
    });

    it('exempts the system for dsdplus, sdr-trunk and trunk-recorder', () => {
        for (const type of ['dsdplus', 'sdr-trunk', 'trunk-recorder']) {
            const row = { ...validRow(), type, systemId: null, talkgroupId: null };

            assert.deepEqual(validateDirwatchRow(row, [row]), {});
        }
    });

    it('requires a talkgroup for a default dirwatch without #TG', () => {
        const row = { ...validRow(), talkgroupId: null, mask: '#SYS_#DATE' };

        assert.deepEqual(validateDirwatchRow(row, [row]), { talkgroupId: 'Talkgroup is required' });
    });

    it('exempts the talkgroup when the mask contains #TG', () => {
        const row = { ...validRow(), talkgroupId: null, mask: '#TG_#DATE' };

        assert.deepEqual(validateDirwatchRow(row, [row]), {});
    });

    it('exempts the talkgroup when the mask contains #TGHZ (legacy substring match)', () => {
        const row = { ...validRow(), talkgroupId: null, mask: '#TGHZ_#DATE' };

        assert.deepEqual(validateDirwatchRow(row, [row]), {});
    });

    it('reports every failing field at once', () => {
        const row = { ...validRow(), directory: '', delay: 0, frequency: -1 };

        assert.deepEqual(validateDirwatchRow(row, [row]), {
            directory: 'Directory is required',
            delay: 'Delay value cannot be less than 2000 milliseconds.',
            frequency: 'Invalid frequency',
        });
    });
});

describe('domain/dirwatch — helpers', () => {
    it('maskHasToken matches known tokens as substrings', () => {
        assert.equal(maskHasToken('#TGAFS_#DATE', '#TG'), true);
        assert.equal(maskHasToken('#TGAFS_#DATE', '#TGAFS'), true);
        assert.equal(maskHasToken('#DATE', '#TG'), false);
        assert.equal(maskHasToken(undefined, '#TG'), false);
    });

    it('validExtension accepts alphanumeric values and empty values', () => {
        assert.equal(validExtension('mp3'), true);
        assert.equal(validExtension('wav'), true);
        assert.equal(validExtension(''), true);
        assert.equal(validExtension(undefined), true);
        assert.equal(validExtension('mp3.'), false);
        assert.equal(validExtension('m p3'), false);
    });

    it('validDirectory rejects empty, network and duplicate directories', () => {
        const rows = [validRow(), { ...validRow(), order: 2 }];

        assert.equal(validDirectory('/var/rdio/unique', rows), true);
        assert.equal(validDirectory('', rows), false);
        assert.equal(validDirectory('\\\\server\\share', rows), false);
        assert.equal(validDirectory('/var/rdio/ingest', rows), false);
    });
});

// the server clamps stored delays to 1440; the section rule rejects below 2000
// no delay passes both, so loading clamps with Math.max(2000, delay)
describe('normalizeDirwatchRow (the legacy form-creation delay clamp)', () => {
    it('loads the delay the server clamps to one day as the floor the rule accepts', () => {
        const row = normalizeDirwatchRow({ ...validRow(), delay: 1440 });

        assert.equal(row.delay, 2000);
    });

    it('loads a delay above the floor as it is', () => {
        const row = normalizeDirwatchRow({ ...validRow(), delay: 2500 });

        assert.equal(row.delay, 2500);
    });

    it('loads an absent delay as it is', () => {
        const row = normalizeDirwatchRow({ ...validRow(), delay: undefined });

        assert.equal(row.delay, undefined);
    });

    it('leaves the rest of the row exactly as it is', () => {
        const row = normalizeDirwatchRow({ ...validRow(), delay: 1440 });

        assert.deepEqual({ ...row, delay: validRow().delay }, validRow());
    });
});
