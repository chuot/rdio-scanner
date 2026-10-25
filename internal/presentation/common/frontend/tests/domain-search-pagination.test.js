// Server and client page math for the search pagination.
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { clientSlice, needsRefetch, serverOffset } from '../domain/search-pagination.js';

const SERVER_PAGE = 200;
const CLIENT_PAGE = 10;

describe('domain/search-pagination — server pages of 200 vs client pages of 10', () => {
    it('maps client page indexes to the server page containing them', () => {
        assert.equal(serverOffset(0, CLIENT_PAGE, SERVER_PAGE), 0);
        assert.equal(serverOffset(19, CLIENT_PAGE, SERVER_PAGE), 0);
        assert.equal(serverOffset(20, CLIENT_PAGE, SERVER_PAGE), 200);
        assert.equal(serverOffset(45, CLIENT_PAGE, SERVER_PAGE), 400);
    });

    it('requests a refetch when the client window leaves the fetched server page', () => {
        assert.equal(needsRefetch(200, 0, SERVER_PAGE), true);
        assert.equal(needsRefetch(199, 0, SERVER_PAGE), false);
        assert.equal(needsRefetch(0, 200, SERVER_PAGE), true);
        assert.equal(needsRefetch(200, 200, SERVER_PAGE), false);
    });

    it('slices the client page out of the fetched server page and pads with nulls', () => {
        const results = Array.from({ length: 200 }, (_, i) => ({ id: i }));

        const page = clientSlice(results, 195, 204, SERVER_PAGE, CLIENT_PAGE);

        assert.equal(page.length, CLIENT_PAGE);
        assert.deepEqual(page.map((c) => (c === null ? null : c.id)), [195, 196, 197, 198, 199, 0, 1, 2, 3, 4]);
    });

    it('pads a short tail page with nulls', () => {
        const results = Array.from({ length: 12 }, (_, i) => ({ id: i }));
        const page = clientSlice(results, 10, 19, SERVER_PAGE, CLIENT_PAGE);

        assert.equal(page.length, CLIENT_PAGE);
        assert.deepEqual(page.map((c) => (c === null ? null : c.id)), [10, 11, null, null, null, null, null, null, null, null]);
    });
});
