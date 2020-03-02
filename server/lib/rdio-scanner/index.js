'use strict';

const Sequelize = require('sequelize');

const config = require('../../config');

const pruneScheduler = require('./helpers/prune-scheduler');
const PubSub = require('./helpers/pubsub');

const GraphQL = require('./graphql');
const Models = require('./models');
const Routes = require('./routes');

const env = process.env.NODE_ENV || 'development';

class RdioScanner {
    static get GraphQL() {
        return GraphQL;
    }

    static get Models() {
        return Models;
    }

    static get PubSub() {
        return PubSub;
    }

    static get Routes() {
        return Routes;
    }

    constructor(httpServer, router, pubsub) {
        this.pubsub = pubsub || new PubSub();

        this.sequelize = new Sequelize(config[env]);

        this.models = new Models(this.sequelize);

        this.graphQL = new GraphQL(this.models, this.pubsub);

        this.graphQL.applyMiddleware({ app: router, cors: true });
        this.graphQL.installSubscriptionHandlers(httpServer);

        this.routes = new Routes(this.models, this.pubsub, router);

        pruneScheduler(this.models);
    }
}

module.exports = RdioScanner;
