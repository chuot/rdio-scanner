// the administration page asks its operator before a new version reaches
// the screen, with the same question the listener page asks and the same
// two answers, through the one prompt module both applications share.

import './dom-shim.js';

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { afterEach, describe, it } from 'node:test';

import { findAll } from './dom-shim.js';

const ENTRY_POINT = new URL('../main.js', import.meta.url);

// The module both applications ask through, and where it sits: beside the shared dialog it opens,
// because a prompt is presentation, not infrastructure.
const SHARED_PROMPT = new URL('../../../common/frontend/presentation/update-prompt.js', import.meta.url);

// The administration page's own sentence, in the words the administration guide uses for the
// application throughout: the operator is asked in the name of the thing they are using.
const TITLE = 'New version available';
const QUESTION = 'A new version of the administration interface is available. Would you like to reload the page?';
const ANSWERS = ['No', 'Yes'];

// The notice a waiting worker posts to the page it serves.
const NOTICE = { type: 'AppUpdateAvailable' };

const SHIMMED_NAVIGATOR = globalThis.navigator;

let freshPage = 0;

// The page the prompt is installed on: its own registration with its own waiting worker, its own
// reload count, and nothing else standing in for what a browser does.
function standingPage({ waiting = true } = {}) {
  const posted = [];
  const reloads = [];
  const listeners = [];

  const registration = {
    waiting: waiting ? { postMessage: (message) => posted.push(message) } : null,
  };

  globalThis.document.location = { reload: () => reloads.push('reload') };

  Object.defineProperty(globalThis, 'navigator', {
    configurable: true,
    value: {
      serviceWorker: {
        addEventListener: (type, handler) => {
          if (type === 'message') listeners.push(handler);
        },
        getRegistration: async () => registration,
      },
    },
  });

  return {
    posted,
    reloads,
    asksInstalled: () => listeners.length,
    deliver: (data) => listeners.forEach((handler) => handler({ data, source: null })),
  };
}

// One page's worth of the prompt. The guard that keeps a question to one per page lives on the page,
// so a case that asks has to be given a page that has not asked yet.
async function promptedPage(options) {
  const page = standingPage(options);

  let prompt;

  try {
    prompt = await import(`${SHARED_PROMPT.href}?page=${freshPage++}`);
  } catch (error) {
    if (error.code === 'ERR_MODULE_NOT_FOUND') {
      assert.fail(`the shared update prompt is not there to install: ${SHARED_PROMPT.pathname}`);
    }

    throw error;
  }

  assert.equal(typeof prompt.promptForUpdate, 'function', 'the shared module exports no prompt to install');

  prompt.promptForUpdate(QUESTION);

  return page;
}

function dialogs() {
  return findAll(document.body, (node) => node.tag === 'rdio-dialog');
}

// The question as the operator sees it: what the dialog is named for, what it says, and the answers it
// offers in the order they stand on the page.
function asked() {
  const open = dialogs();

  assert.equal(open.length, 1, `the page shows ${open.length} update questions, want one`);

  const dialog = open[0];
  const body = dialog.querySelector('.rdio-dialog-body');

  assert.ok(body, 'the update question carries no body to read the question out of');

  return {
    dialog,
    title: dialog.getAttribute('aria-label'),
    question: body.textContent,
    answers: findAll(dialog, (node) => node.tag === 'button').map((button) => button.textContent.trim()),
  };
}

async function answer(label) {
  const { dialog } = asked();
  const button = findAll(dialog, (node) => node.tag === 'button' && node.textContent.trim() === label)[0];

  assert.ok(button, `the update question offers no ${label} answer`);

  button.click();

  // The answer is answered by an action that waits for the registration before it reloads, so the
  // page is given the turn it needs to finish what the press asked for.
  await new Promise((resolve) => setImmediate(resolve));
}

afterEach(() => {
  for (const dialog of dialogs()) dialog.remove();

  Object.defineProperty(globalThis, 'navigator', { configurable: true, value: SHIMMED_NAVIGATOR });
});

describe('common/presentation/update-prompt — the question the administration page asks', () => {
  it('asks its operator in this application\'s own words', async () => {
    const page = await promptedPage();

    page.deliver(NOTICE);

    const question = asked();

    assert.equal(question.title, TITLE, 'the question is named for something other than a new version');
    assert.equal(question.question, QUESTION, 'the operator is asked with other words than this page uses');
    assert.deepEqual(question.answers, ANSWERS, 'the question does not offer No and Yes in that order');
    assert.equal(page.reloads.length, 0, 'the page reloaded before the operator answered');
    assert.deepEqual(page.posted, [], 'something was posted to the waiting worker before an answer');
  });

  it('hands the waiting worker to the page and reloads only when the operator says yes', async () => {
    const page = await promptedPage();

    page.deliver(NOTICE);

    await answer('Yes');

    assert.deepEqual(page.posted, [{ type: 'SKIP_WAITING' }], 'saying yes did not hand this page\'s waiting worker its skip');
    assert.equal(page.reloads.length, 1, 'saying yes did not reload the page once');
    assert.equal(dialogs().length, 0, 'the question is still on the page after it was answered');
  });

  it('posts nothing and reloads nothing when the operator says no', async () => {
    const page = await promptedPage();

    page.deliver(NOTICE);

    await answer('No');

    assert.deepEqual(page.posted, [], 'saying no posted something to the waiting worker');
    assert.equal(page.reloads.length, 0, 'saying no reloaded the page');
    assert.equal(dialogs().length, 0, 'the question is still on the page after it was refused');
  });

  it('leaves a worker that is not waiting alone', async () => {
    const page = await promptedPage({ waiting: false });

    page.deliver(NOTICE);

    await answer('Yes');

    assert.deepEqual(page.posted, [], 'a page with no waiting worker posted to one anyway');
    assert.equal(page.reloads.length, 1, 'a page with no waiting worker stopped reloading itself');
  });

  it('asks once, however many notices arrive', async () => {
    const page = await promptedPage();

    page.deliver(NOTICE);
    page.deliver(NOTICE);
    page.deliver(NOTICE);

    assert.equal(dialogs().length, 1, 'a second notice opened a second question beside the first');
    assert.equal(page.reloads.length, 0, 'a notice reloaded the page on its own');

    await answer('Yes');

    assert.deepEqual(page.posted, [{ type: 'SKIP_WAITING' }], 'the one question asked did not reach its waiting worker');
    assert.equal(page.reloads.length, 1, 'the one question asked did not reload the page once');
  });

  it('installs its listener on the page it is asked for', async () => {
    const page = standingPage();
    const prompt = await import(`${SHARED_PROMPT.href}?page=${freshPage++}`).catch(() => null);

    assert.ok(prompt, `the shared update prompt is not there to install: ${SHARED_PROMPT.pathname}`);
    assert.equal(page.asksInstalled(), 0, 'a page that was not prompted still listens for a worker notice');

    prompt.promptForUpdate(QUESTION);

    assert.equal(page.asksInstalled(), 1, 'the page was not left listening for its worker\'s notice');

    page.deliver({ type: 'SomethingElse' });

    assert.equal(dialogs().length, 0, 'a notice that is not about an update opened a question');
  });
});

describe('admin/frontend/main.js — the administration entry point asks before it boots', () => {
  const source = () => readFileSync(ENTRY_POINT, 'utf8');

  it('imports the shared prompt', () => {
    const text = source();

    assert.ok(
      text.includes("from '../../common/frontend/presentation/update-prompt.js'"),
      'the administration entry point does not import the shared update prompt',
    );
    assert.ok(text.includes('promptForUpdate('), 'the administration entry point does not call the shared update prompt');
  });

  it('passes its own question to it', () => {
    assert.ok(
      source().includes(`promptForUpdate('${QUESTION}')`),
      'the administration entry point asks with other words than the ones its operators should be asked with',
    );
  });

  it('is asked before the page boots its use cases', () => {
    const text = source();
    const askedAt = text.indexOf('promptForUpdate(');
    const bootedAt = text.indexOf('getAdminContext().useCases.boot()');

    assert.ok(askedAt >= 0, 'the administration entry point never asks its operator');
    assert.ok(bootedAt >= 0, 'the administration entry point never boots its use cases, so nothing answers a question');
    assert.ok(
      askedAt < bootedAt,
      'the administration page boots its sections before its operator is asked about a new version',
    );
  });

  it('keeps no dialog of its own', () => {
    const text = source();

    for (const held of ['openDialog', "addEventListener('message'", 'SKIP_WAITING', 'AppUpdateAvailable', 'updatePrompted']) {
      assert.ok(!text.includes(held), `the administration entry point still carries ${held} of its own`);
    }

    assert.ok(!text.includes(TITLE), `the administration entry point still names the question ${TITLE} itself`);
  });
});
