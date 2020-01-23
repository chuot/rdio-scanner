'use strict';

module.exports = {

    up: async (queryInterface) => {
        const transaction = await queryInterface.sequelize.transaction();

        try {
            await queryInterface.addIndex('rdioScannerCalls', ['system'], { transaction });

            await queryInterface.addIndex('rdioScannerCalls', ['system', 'talkgroup'], { transaction });

            await transaction.commit();

        } catch (err) {
            await transaction.rollback();

            throw err;
        }
    },

    down: async (queryInterface) => {
        const transaction = queryInterface.sequelize.transaction();

        try {
            await queryInterface.removeIndex('rdioScannerCalls', ['system'], { transaction });

            await queryInterface.removeIndex('rdioScannerCalls', ['system', 'talkgroup'], { transaction });

            await transaction.commit();

        } catch (err) {
            await transaction.rollback();

            throw err;
        }
    },
};
