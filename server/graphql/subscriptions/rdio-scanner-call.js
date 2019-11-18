'use strict';

const { withFilter } = require('apollo-server-express');

module.exports = ({ pubsub }) => ({
    subscribe: withFilter(
        () => pubsub.asyncIterator(['rdioScannerCall']),
        (payload, variables) => {
            if (variables.selection) {
                if (payload.rdioScannerCall) {
                    const call = payload.rdioScannerCall;

                    let selection;

                    try {
                        selection = JSON.parse(variables.selection);

                    } catch (err) {
                        return false;
                    }

                    return selection[call.system][call.talkgroup];

                } else {
                    return false;
                }

            } else {
                return true;
            }
        },
    ),
});
