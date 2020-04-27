'use strict';

const { DataTypes, Model } = require('sequelize');

class RdioScannerCall extends Model { }

function rdioScannerCallFactory(sequelize) {
    RdioScannerCall.init({
        audio: {
            type: DataTypes.BLOB('long'),
            allowNull: false,
        },
        audioName: {
            type: DataTypes.STRING,
            allowNull: false,
        },
        audioType: {
            type: DataTypes.STRING,
            allowNull: true,
        },
        emergency: {
            type: DataTypes.BOOLEAN,
            defaultValue: false,
            allowNull: false,
        },
        freq: {
            type: DataTypes.INTEGER,
            allowNull: false,
        },
        freqList: {
            type: DataTypes.JSON,
            defaultValue: [],
            allowNull: false,
            get() {
                let freqList = this.getDataValue('freqList');

                if (typeof freqlist === 'string') {
                    try {
                        freqList = JSON.parse(freqList);

                    } catch (error) {
                        freqList = [];
                    }
                }

                return freqList;
            },
        },
        startTime: {
            type: DataTypes.DATE,
            allowNull: false,
        },
        stopTime: {
            type: DataTypes.DATE,
            allowNull: false,
        },
        srcList: {
            type: DataTypes.JSON,
            defaultValue: [],
            allowNull: false,
            get() {
                let srclist = this.getDataValue('srcList');

                if (typeof srclist === 'string') {
                    try {
                        srclist = JSON.parse(srclist);

                    } catch (error) {
                        srclist = [];
                    }
                }

                return srclist;
            },
        },
        system: {
            type: DataTypes.INTEGER,
            allowNull: false,
        },
        talkgroup: {
            type: DataTypes.INTEGER,
            allowNull: false,
        },
    }, {
        modelName: 'rdioScannerCall',
        sequelize,
    });

    return RdioScannerCall;
}

module.exports = rdioScannerCallFactory;
