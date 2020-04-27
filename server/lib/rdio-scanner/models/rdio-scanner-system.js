'use strict';

const { DataTypes, Model } = require('sequelize');

class RdioScannerSystem extends Model { }

function rdioScannerSystemFactory(sequelize) {
    RdioScannerSystem.init({
        aliases: {
            type: DataTypes.JSON,
            defaultValue: [],
            allowNull: false,
        },
        name: {
            type: DataTypes.STRING,
            allowNull: false,
        },
        system: {
            type: DataTypes.INTEGER,
            allowNull: false,
        },
        talkgroups: {
            type: DataTypes.JSON,
            defaultValue: [],
            allowNull: false,
            get() {
                let talkgroups = this.getDataValue('talkgroups');

                if (typeof talkgroups === 'string') {
                    try {
                        talkgroups = JSON.parse(talkgroups);

                    } catch (error) {
                        talkgroups = [];
                    }
                }

                return talkgroups;
            },
        },
    }, {
        modelName: 'rdioScannerSystem',
        sequelize,
    });

    return RdioScannerSystem;
}

module.exports = rdioScannerSystemFactory;
