// tests for domain/playback-queue: calls entering, waiting, and leaving
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import {
    clearQueue,
    enqueue,
    filterActive,
    playbackQueueCount,
    shift,
} from '../domain/playback-queue.js';

describe('domain/playback-queue', () => {
    const callA = { id: 1, system: 10, talkgroup: 100 };
    const callB = { id: 2, system: 10, talkgroup: 101 };
    const callC = { id: 3, system: 20, talkgroup: 200, patches: [100] };

    it('enqueue appends normally and unshifts on priority', () => {
        let queue = enqueue([], callA);
        queue = enqueue(queue, callB);
        queue = enqueue(queue, callC, { priority: true });

        assert.deepEqual(queue.map((c) => c.id), [3, 1, 2]);
    });

    it('shift takes the head of the queue', () => {
        const queue = enqueue(enqueue([], callA), callB);
        const [next, rest] = shift(queue);

        assert.equal(next.id, 1);
        assert.deepEqual(rest.map((c) => c.id), [2]);
    });

    it('clear empties the queue (live feed disable clears the listening queue)', () => {
        assert.deepEqual(clearQueue(enqueue([], callA)), []);
    });

    it('filterActive keeps calls whose talkgroup or a patch is active', () => {
        const map = { 10: { 100: { active: true } } };
        const queue = [callA, callB, callC];
        const kept = filterActive(queue, map);

        assert.deepEqual(kept.map((c) => c.id), [1, 3]);
    });

    it('playbackQueueCount counts the calls that will play up to the newest (descending page)', () => {
        const list = {
            count: 25,
            options: { sort: -1, offset: 200, limit: 200 },
            results: [{ id: 5 }, { id: 6 }, { id: 7 }],
        };

        assert.equal(playbackQueueCount(list, 6), 200 + 1 + 1);
    });

    it('playbackQueueCount counts the calls that will play up to the newest (ascending page)', () => {
        const list = {
            count: 25,
            options: { sort: 1, offset: 0, limit: 200 },
            results: [{ id: 5 }, { id: 6 }, { id: 7 }],
        };

        assert.equal(playbackQueueCount(list, 6), 25 - 0 - 1);
    });

    it('playbackQueueCount counts the call being played: the newest call of the result set reads 1, in both sort orders', () => {
        const descending = { count: 3, options: { sort: -1, offset: 0, limit: 200 }, results: [{ id: 5 }, { id: 6 }, { id: 7 }] };
        const ascending = { count: 3, options: { sort: 1, offset: 0, limit: 200 }, results: [{ id: 5 }, { id: 6 }, { id: 7 }] };

        assert.equal(playbackQueueCount(descending, 5), 1, 'the first row of a descending page is its newest call');
        assert.equal(playbackQueueCount(ascending, 7), 1, 'the last row of an ascending page is its newest call');
    });

    it('playbackQueueCount counts the calls the search filters left in the history', () => {
        const filtered = {
            count: 4,
            options: { sort: -1, offset: 0, limit: 200, system: 10001, talkgroup: 1514200 },
            results: [{ id: 12 }, { id: 9 }, { id: 6 }, { id: 3 }],
        };

        assert.equal(playbackQueueCount(filtered, 9), 2, 'the call being played and the one newer than it');
    });

    it('playbackQueueCount is 0 for an unknown id', () => {
        const list = { count: 1, options: { sort: -1, offset: 0, limit: 200 }, results: [{ id: 5 }] };

        assert.equal(playbackQueueCount(list, 99), 0);
    });
});
