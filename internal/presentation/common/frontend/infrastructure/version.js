// Build-time version injection point: the bundle build defines
// __RDIO_VERSION__ from the presentation workspace manifest.

export const version = typeof __RDIO_VERSION__ === 'string' ? __RDIO_VERSION__ : 'dev';
