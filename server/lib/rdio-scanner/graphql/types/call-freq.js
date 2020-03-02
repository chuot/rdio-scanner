'use strict';

const schema = `
    type RdioScannerCallFreq {
        errorCount: Int
        freq: Int
        len: Int
        pos: Float
        spikeCount: Int
    }
`;

module.exports = { schema };
