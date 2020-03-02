'use strict';

const { ApolloServer, gql } = require('apollo-server-express');

const DataSources = require('./datasources');
const Queries = require('./queries');
const Scalars = require('./scalars');
const Subscriptions = require('./subscriptions');
const Types = require('./types');

class GraphQL {
    static get DataSources() {
        return DataSources;
    }

    static get Queries() {
        return Queries;
    }

    static get Scalars() {
        return Scalars;
    }

    static get Subscriptions() {
        return Subscriptions;
    }

    static get Types() {
        return Types;
    }

    constructor(models, pubsub) {
        const dataSources = () => new DataSources(models);

        const queries = new Queries(models);

        const scalars = new Scalars(models);

        const subscriptions = new Subscriptions(pubsub);

        const types = new Types(models);

        const typeDefs = gql`
            ${scalars.schemas}

            ${types.schemas}

            type Query {
                ${queries.schemas}
            }

            type Subscription {
                ${subscriptions.schemas}
            }
        `;

        const resolvers = Object.assign({},
            { Query: queries.resolvers },
            { Subscription: subscriptions.resolvers },
        );

        return new ApolloServer({ dataSources, resolvers, typeDefs });
    }
}

module.exports = GraphQL;
