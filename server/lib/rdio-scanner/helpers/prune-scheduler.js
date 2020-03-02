'use strict';

const PRUNE_DAYS = parseInt(process.env.RDIO_PRUNEDAYS, 10);

function pruneScheduler(models) {
    const pruneDays = isNaN(PRUNE_DAYS) ? 7 : PRUNE_DAYS;

    if (models === null || typeof models !== 'object' || pruneDays < 1) {
        return null;
    }

    const Op = (models.Sequelize && models.Sequelize.Op) || {};

    return setInterval(() => {
        const now = new Date();

        models.rdioScannerCall.destroy({
            where: {
                startTime: {
                    [Op.lt]: new Date(now.getFullYear(), now.getMonth(), now.getDate() - pruneDays),
                },
            },
        });
    }, 15 * 60 * 1000);
}

module.exports = pruneScheduler;
