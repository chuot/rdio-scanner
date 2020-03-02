'use strict';

const schema = `
    rdioScannerCalls(
        date: RdioScannerDate
        limit: Int
        offset: Int
        sort: Int
        system: Int
        talkgroup: Int
    ): RdioScannerCallsQueryResponse
`;

function resolver() {
    return {
        async rdioScannerCalls(_, { date, limit, offset, sort, system, talkgroup }, { dataSources }) {
            const { count, dateStart, dateStop, results } = await dataSources.rdioScanner.getCalls({ date, system, talkgroup }, { limit, offset, sort });

            return { count, dateStart, dateStop, results };
        },
    };
}

module.exports = { resolver, schema };
