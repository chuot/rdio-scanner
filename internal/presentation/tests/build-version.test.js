// tests for the build version: the version string travels through unchanged
import assert from 'node:assert/strict';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { after, describe, it } from 'node:test';
import { join } from 'node:path';

import { resolveVersion } from '../build.mjs';

// Three routes, in the order they are asked. The build tool names the version it resolved; a developer
// running npm on its own gets the core the repository declared; and a tree that has neither says `dev`
// out loud instead of inventing a release number.
//
// A root directory is passed in rather than found, because the file being read is the repository's, and
// a test must not depend on where it happened to be run from.

const scratchDir = join(import.meta.dirname, '.scratch-version-root');

function scratchRoot(contents) {
    rmSync(scratchDir, { recursive: true, force: true });
    mkdirSync(scratchDir, { recursive: true });

    if (contents !== undefined) {
        writeFileSync(join(scratchDir, 'VERSION.txt'), contents);
    }

    return scratchDir;
}

// A test that makes a directory in the repository takes it back out again.
after(() => {
    rmSync(scratchDir, { recursive: true, force: true });
});

describe('build.mjs — the version a bundle carries', () => {
    it('takes the version the build tool named, exactly as it named it', () => {
        assert.equal(resolveVersion({ RDIO_VERSION: '8.0.0+gdb79311' }, scratchRoot('8.0.0\n')), '8.0.0+gdb79311');
        assert.equal(resolveVersion({ RDIO_VERSION: '8.0.0-beta1+gdb79311' }, scratchRoot('8.0.0\n')), '8.0.0-beta1+gdb79311');
    });

    it('does not shorten the version it was given to its core', () => {
        // The bundle reports the build it is, and a bundle that reported 8.0.0 while being built from a
        // prerelease would be describing a release that has not happened.
        assert.equal(resolveVersion({ RDIO_VERSION: '8.0.0-beta1+gdb79311' }, scratchRoot()), '8.0.0-beta1+gdb79311');
    });

    it('takes the declared core when the build tool said nothing', () => {
        assert.equal(resolveVersion({}, scratchRoot('8.0.0\n')), '8.0.0');
    });

    it('treats an empty version in the environment as nothing said', () => {
        assert.equal(resolveVersion({ RDIO_VERSION: '' }, scratchRoot('8.0.0\n')), '8.0.0');
        assert.equal(resolveVersion({ RDIO_VERSION: '   ' }, scratchRoot('8.0.0\n')), '8.0.0');
    });

    it('says dev when neither the tool nor the repository said a version', () => {
        assert.equal(resolveVersion({}, scratchRoot()), 'dev');
        assert.equal(resolveVersion({ RDIO_VERSION: '' }, scratchRoot()), 'dev');
    });

    it('refuses a declared version it cannot read rather than making the best of it', () => {
        for (const contents of ['v8.0.0', '8.0', '8.0.0-beta1', '', '8.0.0 9.0.0']) {
            assert.throws(
                () => resolveVersion({}, scratchRoot(contents)),
                /VERSION\.txt/,
                `VERSION.txt holding ${JSON.stringify(contents)} was accepted`,
            );
        }
    });

    it('reads the file at the root it was given, not the one beside it', () => {
        // The client sits three directories below the root, so a build that read a VERSION.txt beside the
        // client would read a file the repository does not have.
        assert.equal(resolveVersion({}, scratchRoot('9.1.2\n')), '9.1.2');
    });
});
