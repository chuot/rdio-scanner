import assert from 'node:assert/strict';
import { describe, it } from 'node:test';

import { computeTodos } from '../domain/todos.js';

describe('domain/todos — exact legacy messages', () => {
    const fullConfig = { systems: [{ id: 1 }], apikeys: [{ id: 1 }], dirwatch: [] };

    it('warns about the default admin password first', () => {
        const todos = computeTodos(fullConfig, { passwordNeedChange: true });

        assert.deepEqual(todos, [
            {
                level: 'warn',
                message: 'You are using the default admin password, please change it from the tools / admin password menu.',
            },
        ]);
    });

    it('reports missing systems', () => {
        const todos = computeTodos({ systems: [], apikeys: [{ id: 1 }], dirwatch: [] }, {});

        assert.deepEqual(todos.map((t) => t.level), ['info']);
        assert.equal(todos[0].message, 'No systems defined. Define one from the Systems menu, or turn on Options ▸ Auto Populate to have them created for you.');
    });

    it('names the control that clears the systems todo, so the operator can follow the path', () => {
        const todos = computeTodos({ systems: [], apikeys: [{ id: 1 }], dirwatch: [] }, {});

        assert.deepEqual(todos[0].step, { section: 'options', control: 'autoPopulate' });
    });

    it('names no control for a todo that has no control to open', () => {
        const password = computeTodos({ systems: [{ id: 1 }], apikeys: [{ id: 1 }], dirwatch: [] }, { passwordNeedChange: true });
        const ingest = computeTodos({ systems: [{ id: 1 }], apikeys: [], dirwatch: [] }, {});

        assert.equal(password[0].step, undefined);
        assert.equal(ingest[0].step, undefined);
    });

    it('reports missing ingest sources when both apikeys and dirwatch are empty', () => {
        const todos = computeTodos({ systems: [{ id: 1 }], apikeys: [], dirwatch: [] }, {});

        assert.equal(todos.length, 1);
        assert.equal(todos[0].message, 'No apikeys or dirwatch defined. Set at least one to allow ingesting audio files.');
    });

    it('keeps the legacy order: password, systems, ingest', () => {
        const todos = computeTodos({}, { passwordNeedChange: true });

        assert.deepEqual(todos.map((t) => t.level), ['warn', 'info', 'info']);
    });

    it('no todos for a fully configured server', () => {
        assert.deepEqual(computeTodos(fullConfig, { passwordNeedChange: false }), []);
    });
});
