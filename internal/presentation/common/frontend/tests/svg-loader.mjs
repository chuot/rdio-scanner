// The build inlines an `.svg` import as its text
// (`loader: { '.svg': 'text' }`); Node has no such rule, so a test
// reaching the promo badges would stop at the file extension. These
// hooks state the same rule for Node, loaded with `--test --import`.

import { register } from 'node:module';

register('./svg-loader-hooks.mjs', import.meta.url);
