'use strict';

module.exports = ({ store }) => {
    const camelCase = require('camelcase');
    const fs = require('fs');
    const path = require('path');
    const basename = path.basename(__filename);
    const dataSources = {};

    fs.readdirSync(__dirname)
        .filter((file) => (file.indexOf('.') !== 0) && (file !== basename) && (file.slice(-3) === '.js'))
        .map((file) => file.slice(0, -3))
        .forEach((file) => {
            const ds = require(`./${file}`);
            const name = camelCase(file);
            dataSources[name] = new ds({ store });
        });

    return () => dataSources;
};
