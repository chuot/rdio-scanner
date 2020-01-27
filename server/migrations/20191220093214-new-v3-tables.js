'use strict';

module.exports = {

    up: async (queryInterface, Sequelize) => {
        const transaction = await queryInterface.sequelize.transaction();

        try {
            await queryInterface.addColumn('rdioScannerCalls', 'audioName', Sequelize.STRING, { transaction });

            await queryInterface.addColumn('rdioScannerCalls', 'audioType', Sequelize.STRING, { transaction });

            await queryInterface.addColumn('rdioScannerSystems', 'aliases', Sequelize.JSON, { transaction });

            await transaction.commit();

        } catch (err) {
            await transaction.rollback();

            throw err;
        }
    },

    down: async (queryInterface, Sequelize) => {
        const transaction = queryInterface.sequelize.transaction();

        try {
            await transaction.commit();

            await queryInterface.removeColumn('rdioScannerCalls', 'audioName', Sequelize.STRING, { transaction });

            await queryInterface.removeColumn('rdioScannerCalls', 'audioType', Sequelize.STRING, { transaction });

            await queryInterface.removeColumn('rdioScannerSystems', 'aliases', Sequelize.JSON, { transaction });

        } catch (err) {
            await transaction.rollback();

            throw err;
        }
    },
};
