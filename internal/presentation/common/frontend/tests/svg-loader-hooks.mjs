// The resolve/load pair behind `svg-loader.mjs`: an `.svg` specifier is resolved to the file it
// names and loaded as a module whose default export is the file's text, the way
// `loader: { '.svg': 'text' }` hands it to the bundle.

import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

export async function resolve(specifier, context, nextResolve) {
  if (specifier.endsWith('.svg')) {
    return { url: new URL(specifier, context.parentURL).href, format: 'module', shortCircuit: true };
  }

  return nextResolve(specifier, context);
}

export async function load(url, context, nextLoad) {
  if (url.endsWith('.svg')) {
    const markup = await readFile(fileURLToPath(url), 'utf8');

    return { format: 'module', source: `export default ${JSON.stringify(markup)};`, shortCircuit: true };
  }

  return nextLoad(url, context);
}
