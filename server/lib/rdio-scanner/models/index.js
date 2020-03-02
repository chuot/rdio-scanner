'use strict';

const Sequelize = require('sequelize');

const rdioScannerCallFactory = require('./rdio-scanner-call');
const rdioScannerSystemFactory = require('./rdio-scanner-system');

class Models {
    constructor(sequelize) {
        if (!(sequelize instanceof Sequelize)) {
            throw new Error('sequelize must be an instance of Sequelize');
        }

        this.Sequelize = Sequelize;

        this.sequelize = sequelize;

        this.rdioScannerCall = rdioScannerCallFactory(sequelize);
        this.rdioScannerSystem = rdioScannerSystemFactory(sequelize);
    }
}

module.exports = Models;
