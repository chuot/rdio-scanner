'use strict';

const { withFilter } = require('apollo-server-express');

const schema = `
    rdioScannerCall(selection: String): RdioScannerCall
`;

function resolver(pubsub) {
    return {
        rdioScannerCall: {
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
        },
    };
}

module.exports = { resolver, schema };
