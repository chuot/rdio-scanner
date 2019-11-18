'use strict';

module.exports = ({ pubsub }) => ({
    subscribe: () => pubsub.asyncIterator(['rdioScannerSystems']),
});
