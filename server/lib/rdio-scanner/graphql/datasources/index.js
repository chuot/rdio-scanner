'use strict';

const { DataSource } = require('apollo-datasource');

const getCall = require('./get-call');
const getCalls = require('./get-calls');
const getSystems = require('./get-systems');

class DataSources extends DataSource {
    constructor(models) {
        super();

        this.rdioScanner = {
            getCall: getCall(models),
            getCalls: getCalls(models),
            getSystems: getSystems(models),
        };
    }

    initialize(config) {
        this.context = config.context;
    }
}

module.exports = DataSources;
