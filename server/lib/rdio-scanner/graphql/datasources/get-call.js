'use strict';

function getCall(models) {
    return async (id) => {
        const result = await models.rdioScannerCall.findByPk(id);

        return result.dataValues;
    };
}

module.exports = getCall;
