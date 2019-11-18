'use strict';

require('dotenv').config();

const { ApolloServer, PubSub } = require('apollo-server-express');
const cors = require('cors');
const express = require('express');
const helmet = require('helmet');
const http = require('http');
const path = require('path');

const env = process.env.NODE_ENV || 'development';
const host = process.env.NODE_HOST || '0.0.0.0';
const port = parseInt(process.env.NODE_PORT, 10) || 3000;

const store = require('./models');

const pubsub = new PubSub();

const { dataSources, resolvers, typeDefs } = require('./graphql')({ pubsub, store });

const context = async ({ connection, req }) => {
    if (connection) {
        return connection.context;

    } else {
        return {
            token: req.headers.authorization || '',
        };
    }
}

const app = express();

const httpServer = http.createServer(app);

const apolloServer = new ApolloServer({
    context,
    dataSources,
    resolvers,
    typeDefs,
});

const { pruningScheduler } = require('./helpers/rdio-scanner');

const clientRoot = path.join(__dirname, '../client/dist');

pruningScheduler({ store });

app.use(express.json());
app.use(express.urlencoded({ extended: false }));
app.use(express.static(clientRoot));

app.post('/api/trunk-recorder-call-upload', require('./routes/trunk-recorder-call-upload')({ pubsub, store }));
app.post('/api/trunk-recorder-system-upload', require('./routes/trunk-recorder-system-upload')({ pubsub, store }));

app.set('port', port);

if (env !== 'development') {
    app.disable('x-powered-by');

    app.get(/\/|\/index.html/, cors(), require('./routes/main')({ clientRoot }));

    app.use(helmet());
}

apolloServer.applyMiddleware({ app, cors: true });
apolloServer.installSubscriptionHandlers(httpServer);

httpServer.listen(port, host, () => console.log(`Rdio Scanner is running at http://${host}:${port}/`));

module.exports = app;
