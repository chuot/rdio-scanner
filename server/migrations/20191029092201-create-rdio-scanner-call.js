'use strict';

module.exports = {

    up: async (queryInterface, Sequelize) => {
        const transaction = await queryInterface.sequelize.transaction();

        try {
            await queryInterface.createTable('rdioScannerCalls', {
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
                audio: {
                    allowNull: false,
                    type: Sequelize.BLOB('long'),
                },
                emergency: {
                    allowNull: false,
                    type: Sequelize.BOOLEAN,
                },
                freq: {
                    allowNull: false,
                    type: Sequelize.INTEGER,
                },
                freqList: {
                    allowNull: false,
                    type: Sequelize.JSON,
                },
                startTime: {
                    allowNull: false,
                    type: Sequelize.DATE,
                },
                stopTime: {
                    allowNull: false,
                    type: Sequelize.DATE,
                },
                srcList: {
                    allowNull: false,
                    type: Sequelize.JSON,
                },
                system: {
                    allowNull: false,
                    type: Sequelize.INTEGER,
                },
                talkgroup: {
                    allowNull: false,
                    type: Sequelize.INTEGER,
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
