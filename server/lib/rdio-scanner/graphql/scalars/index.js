'use strict';

const audio = require('./audio');
const date = require('./date');

class Scalars {
    constructor() {
        this.schemas = `
            ${audio.schema}
            ${date.schema}
        `;
    }
}

module.exports = Scalars;
