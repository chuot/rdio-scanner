'use strict';

const schema = `
    rdioScannerSystems: [RdioScannerSystem]
`;

function resolver(pubsub) {
    return {
        rdioScannerSystems: {
            subscribe: () => pubsub.asyncIterator(['rdioScannerSystems']),
        },
    };
}

module.exports = { resolver, schema };
