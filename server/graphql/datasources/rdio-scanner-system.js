'use strict';

const { DataSource } = require('apollo-datasource');
const { systemReducer } = require ('../../helpers/rdio-scanner');

class RdioScannerSystem extends DataSource {
    constructor({ store }) {
        super();

        this.store = store;
    }

    initialize(config) {
        this.context = config.context;
    }

    async getSystems() {
        const systems = await this.store.rdioScannerSystem.findAll();
        return systemReducer(systems);
    }
}

module.exports = RdioScannerSystem;
