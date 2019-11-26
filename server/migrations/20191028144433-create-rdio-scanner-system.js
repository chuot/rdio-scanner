'use strict';

module.exports = {

    up: async (queryInterface, Sequelize) => {
        const transaction = await queryInterface.sequelize.transaction();

        try {
            await queryInterface.createTable('rdioScannerSystems', {
                id: {
                    allowNull: false,
                    autoIncrement: true,
                    primaryKey: true,
                    type: Sequelize.INTEGER,
                },
                createdAt: {
                    allowNull: false,
                    type: Sequelize.DATE,
                },
                updatedAt: {
                    allowNull: false,
                    type: Sequelize.DATE,
                },
                name: {
                    allowNull: false,
                    type: Sequelize.STRING,
                },
                system: {
                    allowNull: false,
                    type: Sequelize.INTEGER,
                },
                talkgroups: {
                    allowNull: false,
                    type: Sequelize.JSON,
                }
            }, { transaction });

            await queryInterface.addIndex('rdioScannerSystems', ['system'], { transaction, unique: true });

            await transaction.commit();

        } catch (err) {
            await transaction.rollback();

            throw err;
        }
    },

    down: async (queryInterface) => {
        const transaction = queryInterface.sequelize.transaction();

        try {
            await queryInterface.dropTable('rdioScannerSystems', { transaction });

            await transaction.commit();

        } catch (err) {
            await transaction.rollback();

            throw err;
        }
    },
};
