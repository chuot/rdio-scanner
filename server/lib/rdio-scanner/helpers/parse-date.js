'use strict';

function parseDate(value) {
    if (value instanceof Date) {
        return value;
    } else if (typeof value === 'number') {
        const date = new Date(1970, 0, 1);
        date.setUTCSeconds(value - date.getTimezoneOffset() * 60);
        return date;
    } else {
        return new Date(value);
    }
}

module.exports = parseDate;
