'use strict';

module.exports = () => async (_, __, { dataSources }) => {
    return await dataSources.rdioScannerSystem.getSystems();
};
