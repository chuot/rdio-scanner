'use strict';

const schema = `
    type RdioScannerCallsQueryResponse {
        count: Int
        dateStart: RdioScannerDate
        dateStop: RdioScannerDate
        results: [RdioScannerCall]
    }
`;

module.exports = { schema };
