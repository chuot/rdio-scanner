// Build-time version injection point: esbuild defines
// __RDIO_VERSION__ from client-ng/package.json.

export const version = typeof __RDIO_VERSION__ === 'string' ? __RDIO_VERSION__ : 'dev';
