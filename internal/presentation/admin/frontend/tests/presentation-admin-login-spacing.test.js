// the login card gives its failure message the same spacing step as its fields
// the gap is the card's rule: the shared message shape carries no margin

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const stylesDir = dirname(fileURLToPath(import.meta.url));

function cssRules(stylesheet) {
  const text = stylesheet.replace(/\/\*[\s\S]*?\*\//g, '');

  return [...text.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(([, selector, block]) => ({
    selector: selector.trim().replace(/\s+/g, ' '),
    block: block.trim(),
  }));
}

function adminRules() {
  return cssRules(readFileSync(join(stylesDir, '..', 'styles', 'admin.css'), 'utf8'));
}

function themeRules() {
  return cssRules(readFileSync(join(stylesDir, '..', '..', '..', 'common', 'frontend', 'styles', 'theme.css'), 'utf8'));
}

test('the login card separates its failure message from its Login button', () => {
  const rule = adminRules().find((candidate) => candidate.selector.includes('.rdio-admin-login') && candidate.selector.includes('.rdio-form-error'));

  assert.ok(rule,
    'the login card styles its title, its field, and its button, and gives the failure message it shows no spacing of its own');

  assert.match(rule.block, /margin-top\s*:\s*var\(--rdio-space-\d\)/,
    `the login card's failure message is not spaced by the card's own spacing step: "${rule.block}"`);
});

test('the gap belongs to the login card, not to every form message', () => {
  const shared = themeRules().filter((candidate) => candidate.selector.includes('.rdio-form-error'));

  assert.ok(shared.length > 0, 'the shared stylesheet styles no form message at all');

  for (const rule of shared) {
    assert.equal(/(^|[;\s])margin(-top)?\s*:/.test(rule.block), false,
      `the shared form message carries spacing the listener application's own forms inherit: "${rule.selector} { ${rule.block} }"`);
  }
});
