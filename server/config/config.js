'use strict';

const config = {
    database: process.env.DB_NAME,
    dialect: process.env.DB_DIALECT || 'sqlite',
    dialectOptions: {
        timezone: process.env.DB_TZ || 'Etc/GMT0',
    },
    host: process.env.DB_HOST,
    logging: false,
    password: process.env.DB_PASS,
    port: process.env.DB_PORT,
    storage: process.env.DB_STORAGE || 'database.sqlite',
    username: process.env.DB_USER,
};

module.exports = {
    development: config,
    production: config,
    test: config,
};
