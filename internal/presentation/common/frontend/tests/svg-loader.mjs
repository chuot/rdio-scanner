// The asset import the client build resolves for a stylesheet's icons and the promo badges, made
// answerable to a test run as well. `build.mjs` inlines an `.svg` import as its text
// (`loader: { '.svg': 'text' }`), so the modules that import an asset are importable in the bundle.
// Node has no such rule, so a test that renders the listener shell — which reaches the badges
// through the promo it schedules — would stop at the file extension before it reached any assertion.
//
// This is the same rule, stated for Node's module hooks: an `.svg` import answers with its markup as
// a string, exactly as the bundle ships it. It is loaded with `node --test --import`, so every test
// file in the suite sees it, and it changes nothing about what the build produces.

import { register } from 'node:module';

register('./svg-loader-hooks.mjs', import.meta.url);
