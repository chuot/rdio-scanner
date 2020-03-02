'use strict';

const schema = `
    rdioScannerCall(id: String): RdioScannerCall
`;

function resolver() {
    return {
        async rdioScannerCall(_, { id }, { dataSources }) {
            const rdioScannerCall = await dataSources.rdioScanner.getCall(id);

            return rdioScannerCall;
        },
    };
}

module.exports = { resolver, schema };
