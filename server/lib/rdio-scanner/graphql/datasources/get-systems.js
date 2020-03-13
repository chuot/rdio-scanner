'use strict';

function getSystems(models) {
    return async () => {
        const systems = await models.rdioScannerSystem.findAll({
            order: [['system', 'ASC']],
        });

        return systems;
    };
}

module.exports = getSystems;
