'use script';

const multer = require('multer');

const upload = multer({ storage: multer.memoryStorage() });

const validateApiKey = require('../helpers/validate-api-keys');

class TrunkRecorderSystemUpload {
    constructor(models, pubsub) {
        return {
            path: '/api/trunk-recorder-system-upload',

            middleware: (req, res) => upload.fields([
                { name: 'csv', maxCount: 1 },
                { name: 'key', maxCount: 1 },
                { name: 'name', maxCount: 1 },
                { name: 'system', maxCount: 1 },
            ])(req, res, async (err) => {
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

                    const emit = async () => {
                        const systems = await models.rdioScannerSystem.findAll({ order: [['system', 'ASC']]});

                        pubsub.publish('rdioScannerSystems', { rdioScannerSystems: systems });
                    };

                    if (system.talkgroups.length) {
                        try {
                            const rec = await models.rdioScannerSystem.findOne({ where: { system: system.system } });

                            if (rec) {
                                await rec.update(system);

                            } else {
                                await models.rdioScannerSystem.create(system);
                            }

                            await emit();

                            res.send(`System ${system.name} imported successfully.\n`);

                        } catch (err) {
                            res.send(err.message);
                        }

                    } else {
                        try {
                            await models.rdioScannerSystem.destroy({ where: { system: system.system } })

                            await emit();

                            res.send(`System ${system.name} removed from configuration.\n`);

                        } catch (err) {
                            res.send(`Error while removing system ${system.name}.\n`);
                        }
                    }
                }
            }),
        };
    }
}

module.exports = TrunkRecorderSystemUpload;
