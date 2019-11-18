'use strict';

module.exports = () => async (_, { date, first, last, skip, sort, system, talkgroup }, { dataSources }) => {
    const { count, dateStart, dateStop, results } = await dataSources.rdioScannerCall.getCalls({ date, system, talkgroup }, { first, last, skip, sort });

    return { count, dateStart, dateStop, results };
};
