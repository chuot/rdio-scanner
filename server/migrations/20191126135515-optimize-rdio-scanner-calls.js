'use strict';

module.exports = {

    up: async (queryInterface) => {
        const transaction = await queryInterface.sequelize.transaction();

        try {
            await queryInterface.removeIndex('rdioScannerCalls', ['system'], { transaction });

            await queryInterface.removeIndex('rdioScannerCalls', ['talkgroup'], { transaction });

            await transaction.commit();

        } catch (err) {
            await transaction.rollback();

            throw err;
        }
    },
};

