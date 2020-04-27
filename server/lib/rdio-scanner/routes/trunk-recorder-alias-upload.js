'use strict';

const multer = require('multer');

const upload = multer({ storage: multer.memoryStorage() });

const validateApiKey = require('../helpers/validate-api-keys');

class TrunkRecorderAliasUpload {
    constructor(models, pubsub) {
        return {
            path: '/api/trunk-recorder-alias-upload',

            middleware: (req, res) => upload.fields([
                { name: 'csv', maxCount: 1 },
                { name: 'key', maxCount: 1 },
                { name: 'system', maxCount: 1 },
            ])(req, res, async (err) => {
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

                    const rec = await models.rdioScannerSystem.findOne({ where: { system } });

                    if (rec) {
                        try {
                            await rec.update({ aliases });

                            const systems = await models.rdioScannerSystem.findAll({ order: [['system', 'ASC']] });

                            res.send(`System ${rec.name} updated successfully.\n`);

                            pubsub.publish('rdioScannerSystems', { rdioScannerSystems: systems });

                        } catch (err) {
                            res.send(err.message);
                        }

                    } else {
                        res.send(`Cannot update aliases for system #${system}.\n`);
                    }
                }
            }),
        };
    }
}

module.exports = TrunkRecorderAliasUpload;
