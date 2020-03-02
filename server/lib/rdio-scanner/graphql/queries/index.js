'use strict';

const call = require('./call');
const calls = require('./calls');
const config = require('./config');
const systems = require('./systems');

class Queries {
    constructor(models) {
        this.schemas = `
            ${call.schema}
            ${calls.schema}
            ${config.schema}
            ${systems.schema}
        `;

        this.resolvers = Object.assign({},
            call.resolver(models),
            calls.resolver(models),
            config.resolver(models),
            systems.resolver(models),
        );
    }
}

module.exports = Queries;
