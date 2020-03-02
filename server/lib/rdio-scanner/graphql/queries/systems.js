'use strict';

const schema = `
    rdioScannerSystems: [RdioScannerSystem]
`;

function resolver() {
    return {
        async rdioScannerSystems(_, __, { dataSources }) {
            return await dataSources.rdioScanner.getSystems();
        },
    };
}

module.exports = { resolver, schema };
