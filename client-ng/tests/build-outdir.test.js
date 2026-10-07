import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { dirname, join, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

import { resolveOutDir } from '../build.mjs';

// The client directory the paths below are read from. What is being asserted is which directory a flag lands
// on, relative to the client, so the client directory is the one this test file lives in.
const clientDir = join(dirname(fileURLToPath(import.meta.url)), '..');

const namedWebapp = resolve(clientDir, '..', 'server-ng', 'webapp');

describe('build.mjs — the directory the build writes to', () => {
    it('writes to ../server/webapp when nothing is given, which is what it has always done', () => {
        const outDir = resolveOutDir([]);

        assert.equal(outDir, resolve(clientDir, '..', 'server', 'webapp'));
        assert.equal(resolveOutDir(['--production']), outDir);
    });

    it('writes to the directory --out names, relative to this client directory', () => {
        assert.equal(resolveOutDir(['--out', '../server-ng/webapp'], clientDir), namedWebapp);
    });

    it('takes the directory in the same word as the flag', () => {
        assert.equal(resolveOutDir(['--out=../server-ng/webapp', '--production'], clientDir), namedWebapp);
    });

    it('takes a directory that is already absolute', () => {
        const absolute = join(sep, 'tmp', 'webapp-here');

        assert.equal(resolveOutDir(['--out', absolute], clientDir), absolute);
    });

    it('says so when --out is given without a directory after it', () => {
        assert.throws(() => resolveOutDir(['--out'], clientDir), /--out needs a directory/);
        assert.throws(() => resolveOutDir(['--out', '--production'], clientDir), /--out needs a directory/);
    });
});
