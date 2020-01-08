'use strict';

const multer = require('multer');
const upload = multer({ storage: multer.memoryStorage() });

const { systemReducer, validateApiKey } = require('../helpers/rdio-scanner');

module.exports = ({ pubsub, store }) => (req, res) => upload.fields([
    { name: 'csv', maxCount: 1 },
    { name: 'key', maxCount: 1 },
    { name: 'name', maxCount: 1 },
    { name: 'system', maxCount: 1 },
])(req, res, (err) => {
    if (err instanceof multer.MulterError) {
        res.send(`${err.message}: ${err.field}\n`);

    } else if (err) {
        res.send(`${err.message}\n`);

    } else {
        const key = req.body.key;

        const system = {
            name: req.body.name,
            system: parseInt(req.body.system, 10),
            talkgroups: [],
        }

        if (!validateApiKey(key, system.system)) {
            return res.send(`Api key is invalid for system ${system.system}.\n`);
        }

        const csv = req.files.csv[0].buffer.toString()
            .split(/\n/)
            .filter((l) => l.length && !/^\s*#/.test(l));

        for (const line of csv) {
            const vals = line.split(',');

            const talkgroup = {
                alphaTag: vals[3],
                dec: parseInt(vals[0], 10),
                description: vals[4],
                group: vals[6],
                mode: vals[2],
                tag: vals[5],
            };

            if (validateApiKey(key, system.system, talkgroup.dec)) {
                system.talkgroups.push(talkgroup);
            }
        }

        const emit = () => {
            store.rdioScannerSystem.findAll({ order: [['system', 'ASC']]}).then((systems) => {
                pubsub.publish('rdioScannerSystems', { rdioScannerSystems: systemReducer(systems) });
            })
        };

        if (system.talkgroups.length) {
            system.talkgroups = JSON.stringify(system.talkgroups);

            store.rdioScannerSystem.findOne({ where: { system: system.system } })
                .then((rdioScannerSystem) => {
                    if (rdioScannerSystem) {
                        rdioScannerSystem.update(system).then(() => {
                            emit();
                            res.send(`System ${system.name} updated successfully.\n`);
                        });

                    } else {
                        store.rdioScannerSystem.create(system).then(() => {
                            emit();
                            res.send(`System ${system.name} imported successfully.\n`);
                        });
                    }
                });

        } else {
            store.rdioScannerSystem.destroy({ where: { system: system.system } })
                .then(() => {
                    emit();
                    res.send(`System ${system.name} removed from configuration.\n`);
                })
                .catch(() => res.send(`Error while removing system ${system.name}.\n`));
        }
    }
});
