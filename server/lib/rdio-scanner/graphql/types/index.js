'use strict';

const alias = require('./alias');
const callFreq = require('./call-freq');
const callQueryResponse = require('./call-query-response');
const callSrc = require('./call-src');
const call = require('./call');
const config = require('./config');
const system = require('./system');
const talkgroup = require('./talkgroup');

class Types {
    constructor() {
        this.schemas = `
            ${alias.schema}
            ${callFreq.schema}
            ${callQueryResponse.schema}
            ${callSrc.schema}
            ${call.schema}
            ${config.schema}
            ${system.schema}
            ${talkgroup.schema}
        `;
    }
}

module.exports = Types;
