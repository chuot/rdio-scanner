'use strict';

module.exports = {

    up: async (queryInterface, Sequelize) => {
        const transaction = await queryInterface.sequelize.transaction();

        try {
            await queryInterface.createTable('rdioScannerCalls', {
                id: {
                    type: Sequelize.INTEGER,
                    autoIncrement: true,
                    primaryKey: true,
                    allowNull: false,
                },
                createdAt: {
                    type: Sequelize.DATE,
                    allowNull: false,
                },
                updatedAt: {
                    type: Sequelize.DATE,
                    allowNull: false,
                },
                audio: {
                    type: Sequelize.BLOB('long'),
                    allowNull: false,
                },
                emergency: {
                    type: Sequelize.BOOLEAN,
                    allowNull: false,
                },
                freq: {
                    type: Sequelize.INTEGER,
                    allowNull: false,
                },
                freqList: {
                    type: Sequelize.JSON,
                    allowNull: false,
                },
                startTime: {
                    type: Sequelize.DATE,
                    allowNull: false,
                },
                stopTime: {
                    type: Sequelize.DATE,
                    allowNull: false,
                },
                srcList: {
                    type: Sequelize.JSON,
                    allowNull: false,
                },
                system: {
                    type: Sequelize.INTEGER,
                    allowNull: false,
                },
                talkgroup: {
                    type: Sequelize.INTEGER,
                    allowNull: false,
                },
            }, { transaction });

            await queryInterface.addIndex('rdioScannerCalls', ['startTime'], { transaction });

            await queryInterface.addIndex('rdioScannerCalls', ['system'], { transaction });

            await queryInterface.addIndex('rdioScannerCalls', ['talkgroup'], { transaction });

            await transaction.commit();

        } catch (err) {
            await transaction.rollback();

            throw err;
        }
    },

    down: async (queryInterface) => {
        const transaction = queryInterface.sequelize.transaction();

        try {
            await queryInterface.dropTable('rdioScannerCalls', { transaction });

            await transaction.commit();

        } catch (err) {
            await transaction.rollback();

            throw err;
        }
    },
};
