import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { advanceReplayOffset, recordHistory } from '../src/domain/replay-history.js';

describe('domain/replay-history — last 5 history', () => {
    it('records played calls, newest first, capped at 5', () => {
        let history = new Array(5).fill(null);

        for (let i = 1; i <= 7; i++) {
            history = recordHistory(history, { id: i });
        }

        assert.deepEqual(history.map((c) => c?.id), [7, 6, 5, 4, 3]);
    });

    it('does not duplicate a call already in the history', () => {
        let history = recordHistory(recordHistory(new Array(5).fill(null), { id: 1 }), { id: 1 });

        assert.equal(history.filter((c) => c?.id === 1).length, 1);
    });

    it('ignores null calls', () => {
        const history = recordHistory(new Array(5).fill(null), null);

        assert.deepEqual(history, new Array(5).fill(null));
    });
});

describe('domain/replay-history — replayOffset walking', () => {
    it('first press replays the current call (offset stays 0)', () => {
        assert.equal(advanceReplayOffset(-1, 5), 0);
    });

    it('repeated presses walk the history: 1st, 2nd, 3rd item', () => {
        let offset = advanceReplayOffset(-1, 5);
        offset = advanceReplayOffset(offset, 5);
        assert.equal(offset, 1);
        offset = advanceReplayOffset(offset, 5);
        assert.equal(offset, 2);
    });

    it('the offset never exceeds the history length', () => {
        let offset = 4;
        offset = advanceReplayOffset(offset, 5);
        assert.equal(offset, 5);
        offset = advanceReplayOffset(offset, 5);
        assert.equal(offset, 5);
    });
});
