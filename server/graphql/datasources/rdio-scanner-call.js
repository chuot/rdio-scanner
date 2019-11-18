'use strict';

const { DataSource } = require('apollo-datasource');
const { getPageRange } = require('../../helpers/paginator');
const { callReducer } = require('../../helpers/rdio-scanner');

class RdioScannerSystem extends DataSource {
    constructor({ store }) {
        super();

        this.store = store;
    }

    initialize(config) {
        this.context = config.context;
    }

    async getCall(id) {
        const result = await this.store.rdioScannerCall.findByPk(id);

        return callReducer(result.dataValues);
    }

    async getCalls({ date, system, talkgroup }, { first, last, skip, sort }) {
        const Op = this.store.Sequelize.Op;

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

        const dateStartQuery = await this.store.rdioScannerCall.findOne({ order: [['startTime', 'ASC']], where });

        const dateStart = dateStartQuery && dateStartQuery.get('startTime');

        const dateStopQuery = await this.store.rdioScannerCall.findOne({ order: [['startTime', 'DESC']], where });

        const dateStop = dateStopQuery && dateStopQuery.get('startTime');

        if (typeof date === 'string') {
            const d = new Date(date);

            where.startTime = {
                [Op.gte]: new Date(d.getFullYear(), d.getMonth(), d.getDate(), 0, 0, 0),
                [Op.lte]: new Date(d.getFullYear(), d.getMonth(), d.getDate(), 23, 59, 59),
            };
        }

        const count = await this.store.rdioScannerCall.count({ where });

        const { limit, offset } = getPageRange({ count, first, last, skip });

        const calls = await this.store.rdioScannerCall.findAll({ attributes, limit, offset, order, where });

        const results = callReducer(calls);

        return { count, dateStart, dateStop, results };
    }
}

module.exports = RdioScannerSystem;
