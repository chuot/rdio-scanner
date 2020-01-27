'use strict';

const multer = require('multer');
const upload = multer({ storage: multer.memoryStorage() });

const { systemReducer, validateApiKey } = require('../helpers/rdio-scanner');

module.exports = ({ pubsub, store }) => (req, res) => upload.fields([
    { name: 'csv', maxCount: 1 },
    { name: 'key', maxCount: 1 },
    { name: 'system', maxCount: 1 },
])(req, res, (err) => {
    if (err instanceof multer.MulterError) {
        res.send(`${err.message}: ${err.field}\n`);

    } else if (err) {
        res.send(`${err.message}\n`);

    } else {
        const key = req.body.key;

        const system = parseInt(req.body.system, 10);

        const aliases = [];

        if (!validateApiKey(key, system)) {
            return res.send(`Api key is invalid for system ${system}.\n`);
        }

        const csv = req.files.csv[0].buffer.toString()
            .split(/\n/)
            .filter((l) => l.length && !/^\s*#/.test(l));

        for (const line of csv) {
            const vals = line.split(',');

            aliases.push({
                name: vals[1],
                uid: parseInt(vals[0], 10),
            })
        }

        store.rdioScannerSystem.findOne({ where: { system } })
            .then((rdioScannerSystem) => {
                if (rdioScannerSystem) {
                    rdioScannerSystem.update({ aliases: JSON.stringify(aliases), system }).then(() => {
                        res.send(`System ${rdioScannerSystem.name} updated successfully.\n`);

                        store.rdioScannerSystem.findAll({ order: [['system', 'ASC']] }).then((systems) => {
                            pubsub.publish('rdioScannerSystems', { rdioScannerSystems: systemReducer(systems) });
                        })
                    });

                } else {
                    res.send(`Cannot update aliases for system ${rdioScannerSystem.name}.\n`);
                }
            });
    }
});
