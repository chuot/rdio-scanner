'use strict';

function getPageRange({ count, first, last, skip }) {
    const page = {
        default: 100,
        max: 1000,
    };

    let limit;

    let offset;

    first = typeof first === 'number' && first > 0 ? Math.min(page.max, first) : typeof last !== 'number' ? page.default : null;

    last = typeof last === 'number' && last > 0 ? Math.min(page.max, last) : null;

    skip = typeof skip === 'number' && skip > 0 ? skip : 0;

    if (typeof first === 'number') {
        limit = Math.max(1, Math.min(count, skip + first) - skip);
        offset = Math.min(count - 1, skip);

    } else {
        limit = count - Math.max(0, Math.min(count, skip) - last);
        offset = Math.max(0, Math.max(count, skip) - last);
    }

    return { limit, offset };
}

module.exports = { getPageRange };
