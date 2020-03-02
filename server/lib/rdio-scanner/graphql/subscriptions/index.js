'use strict';

const call = require('./call');
const systems = require('./systems');

class Subscriptions {
    constructor(pubsub) {
        this.schemas = `
            ${call.schema}
            ${systems.schema}
        `;

        this.resolvers = Object.assign({},
            call.resolver(pubsub),
            systems.resolver(pubsub),
        );
    }
}

module.exports = Subscriptions;
