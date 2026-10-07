import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { resolveOutDir } from '../build.mjs';

// The client directory the paths below are read from. What is being asserted is which directory the build
// writes to, relative to the client, so the client directory is the one this test file lives in.
const clientDir = join(dirname(fileURLToPath(import.meta.url)), '..');

describe('build.mjs — the directory the build writes to', () => {
    it('writes to the webapp package\'s bundle, beside this client directory', () => {
        const outDir = resolveOutDir([]);

        assert.equal(outDir, resolve(clientDir, '..', 'webapp', 'bundle'));
        assert.equal(resolveOutDir(['--production']), outDir);
        assert.equal(resolveOutDir(['--production', '--watch']), outDir);
    });

    it('has no flag that points it somewhere else, because there is one server', () => {
        assert.throws(() => resolveOutDir(['--out', '../somewhere/else'], clientDir), /one server/);
        assert.throws(() => resolveOutDir(['--out=../somewhere/else'], clientDir), /one server/);
    });
});
