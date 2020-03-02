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
                    type: Sequelize.DATE,
                    allowNull: false,
                },
                updatedAt: {
                    type: Sequelize.DATE,
                    allowNull: false,
                },
                name: {
                    type: Sequelize.STRING,
                    allowNull: false,
                },
                system: {
                    type: Sequelize.INTEGER,
                    allowNull: false,
                },
                talkgroups: {
                    type: Sequelize.JSON,
                    defaultValue: [],
                    allowNull: false,
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
