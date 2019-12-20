'use strict';

require('dotenv').config();

const defaultConfig = {
    database: '',
    dialect: 'sqlite',
    dialectOptions: {
        timezone: process.env.DB_TZ || 'Etc/GMT0',
    },
    host: '',
    logging: false,
    password: '',
    port: '',
    storage: 'database.sqlite',
    username: '',
};

function getConfig() {
    return {
        database: process.env.DB_NAME || defaultConfig.database,
        dialect: process.env.DB_DIALECT || defaultConfig.dialect,
        dialectOptions: defaultConfig.dialectOptions,
        host: process.env.DB_HOST || defaultConfig.host,
        logging: defaultConfig.logging,
        password: process.env.DB_PASS || defaultConfig.password,
        port: process.env.DB_PORT || defaultConfig.port,
        storage: process.env.DB_STORAGE || defaultConfig.storage,
        username: process.env.DB_USER || defaultConfig.username,
    };
}

module.exports = {
    development: getConfig(),
    production: getConfig(),
    test: getConfig(),
};
