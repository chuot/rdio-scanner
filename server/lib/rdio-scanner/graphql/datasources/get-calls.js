'use strict';

const { Op } = require('sequelize');

function getCalls(models) {
    return async ({ date, system, talkgroup }, { limit, offset, sort }) => {
        const attributes = {
            exclude: ['audio', 'freqList', 'srcList'],
        };

        const order = [['startTime', typeof sort === 'number' && sort < 0 ? 'DESC' : 'ASC']];

        const where = {};

        if (typeof system === 'number') {
            where.system = system;
        }

        if (typeof talkgroup === 'number') {
            where.talkgroup = talkgroup;
        }

        const dateStartQuery = await models.rdioScannerCall.findOne({ order: [['startTime', 'ASC']], where });

        const dateStart = dateStartQuery && dateStartQuery.get('startTime');

        const dateStopQuery = await models.rdioScannerCall.findOne({ order: [['startTime', 'DESC']], where });

        const dateStop = dateStopQuery && dateStopQuery.get('startTime');

        if (typeof date === 'string') {
            const d = new Date(date);

            where.startTime = {
                [Op.gte]: new Date(d.getFullYear(), d.getMonth(), d.getDate(), 0, 0, 0),
                [Op.lte]: new Date(d.getFullYear(), d.getMonth(), d.getDate(), 23, 59, 59),
            };
        }

        const count = await models.rdioScannerCall.count({ where });

        limit = typeof limit === 'number' ? limit : 100;
        offset = typeof offset === 'number' ? offset : 0;

        const results = await models.rdioScannerCall.findAll({ attributes, limit, offset, order, where });

        return { count, dateStart, dateStop, results };
    };
}

module.exports = getCalls;