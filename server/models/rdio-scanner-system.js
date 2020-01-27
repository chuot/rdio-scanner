'use strict';

module.exports = (sequelize, DataTypes) => {
    return sequelize.define('rdioScannerSystem', {
        aliases: DataTypes.JSON,
        name: DataTypes.STRING,
        system: {
            type: DataTypes.INTEGER,
            unique: true,
        },
        talkgroups: DataTypes.JSON,
    }, {});
};