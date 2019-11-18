'use strict';

module.exports = (sequelize, DataTypes) => {
    return sequelize.define('rdioScannerSystem', {
        name: DataTypes.STRING,
        system: DataTypes.INTEGER,
        talkgroups: DataTypes.JSON,
    }, {});
};