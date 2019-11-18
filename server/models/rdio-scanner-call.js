'use strict';

module.exports = (sequelize, DataTypes) => {
    return sequelize.define('rdioScannerCall', {
        audio: DataTypes.BLOB('long'),
        emergency: DataTypes.BOOLEAN,
        freq: DataTypes.INTEGER,
        freqList: DataTypes.JSON,
        startTime: DataTypes.DATE,
        stopTime: DataTypes.DATE,
        srcList: DataTypes.JSON,
        system: DataTypes.INTEGER,
        talkgroup: DataTypes.INTEGER,
    }, {});
};