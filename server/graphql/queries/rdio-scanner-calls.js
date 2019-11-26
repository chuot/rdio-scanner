'use strict';

module.exports = () => async (_, { date, limit, offset, sort, system, talkgroup }, { dataSources }) => {
    const { count, dateStart, dateStop, results } = await dataSources.rdioScannerCall.getCalls({ date, system, talkgroup }, { limit, offset, sort });

    return { count, dateStart, dateStop, results };
};
