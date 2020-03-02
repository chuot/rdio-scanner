'use strict';

function getSystems(models) {
    return async () => {
        const systems = await models.rdioScannerSystem.findAll();

        return systems;
    };
}

module.exports = getSystems;
