import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { adminConfig, resolveOutDir, scannerConfig } from '../build.mjs';

// The client directory the paths below are read from. What is being asserted is which directory each build
// writes to, relative to the client, so the client directory is the one this test file lives in.
const clientDir = join(dirname(fileURLToPath(import.meta.url)), '..');

const presentationDir = resolve(clientDir, '..');

describe('build.mjs — the directory each application writes to', () => {
    it('writes the scanner bundle beside the Go package that embeds it', () => {
        assert.equal(scannerConfig.outDir, resolve(clientDir, '..', 'serving', 'scanner', 'bundle'));
    });

    it('writes the admin bundle beside the Go package that embeds it', () => {
        assert.equal(adminConfig.outDir, resolve(clientDir, '..', 'serving', 'admin', 'bundle'));
    });

    it('gives the two applications two directories, both inside the presentation layer', () => {
        assert.notEqual(scannerConfig.outDir, adminConfig.outDir);

        for (const config of [scannerConfig, adminConfig]) {
            assert.ok(
                config.outDir.startsWith(presentationDir + '/'),
                `the ${config.name} bundle is not inside the presentation layer`,
            );
        }
    });

    it('has no flag that points either build somewhere else, because there is one server', () => {
        assert.throws(() => resolveOutDir('../somewhere/else', ['--out', '../somewhere/else']), /--out is gone/);
        assert.throws(() => resolveOutDir('../somewhere/else', ['--out=../somewhere/else']), /--out is gone/);
    });
});
