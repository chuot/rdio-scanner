'use strict';

module.exports = () => async (_, { id }, { dataSources }) => {
    const rdioScannerCall = await dataSources.rdioScannerCall.getCall(id);

    return rdioScannerCall;
};
