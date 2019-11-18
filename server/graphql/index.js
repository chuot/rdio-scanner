'use strict';

const camelCase = require('camelcase');
const fs = require('fs');
const { gql } = require('apollo-server-express');
const path = require('path');

function graphql({ pubsub, store }) {

    const dataSources = require('./datasources')({ store });

    const query = readDir('queries', pubsub);
    const scalar = readDir('scalars', pubsub);
    const subscription = readDir('subscriptions', pubsub);
    const type = readDir('types', pubsub);

    const typeDefs = gql([
        scalar.schemas,
        'type Query {', query.schemas, '}', '',
        'type Subscription {', subscription.schemas, '}', '',
        type.schemas
    ].join('\n'));

    const resolvers = {
        Query: query.resolvers,
        Subscription: subscription.resolvers,
    };

    return {
        dataSources,
        resolvers,
        typeDefs,
    };
}

function readDir(dir, pubsub) {
    let schemas = '';
    const resolvers = {};

    dir = path.resolve(__dirname, dir);

    const filenames = fs.readdirSync(dir);

    filenames.filter((file) => (file.slice(-4) === '.gql'))
        .forEach((file) => {
            const schema = fs.readFileSync(path.join(dir, file));

            schemas += `${schema}\n`;
        })

    filenames.filter((file) => (file.indexOf('.') !== 0) && (file.slice(-3) === '.js'))
        .map((file) => file.slice(0, -3))
        .forEach((file) => {
            const resolver = require(path.join(dir, file))({ pubsub });
            const name = camelCase(file);

            resolvers[name] = resolver;
        });

    return { resolvers, schemas };
}

module.exports = graphql;
