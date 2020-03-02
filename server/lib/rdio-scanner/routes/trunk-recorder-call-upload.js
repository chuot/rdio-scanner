'use strict';

const multer = require('multer');

const upload = multer({ storage: multer.memoryStorage() });

const parseDate = require('../helpers/parse-date');
const validateApiKey = require('../helpers/validate-api-keys');

class TrunkRecorderCallUpload {
    constructor(models, pubsub) {
        return {
            path: '/api/trunk-recorder-call-upload',

            middleware: (req, res) => upload.fields([
                { name: 'audio', maxCount: 1 },
                { name: 'key', maxCount: 1 },
                { name: 'meta', maxCount: 1 },
                { name: 'system', maxCount: 1 },
            ])(req, res, async (err) => {
                if (err instanceof multer.MulterError) {
                    res.send(`${err.message}: ${err.field}\n`);

                } else if (err) {
                    res.send(`${err.message}\n`);

                } else {
                    const key = req.body.key;

                    let meta = req.files.meta[0].buffer.toString();

                    try {
                        meta = JSON.parse(meta);

                    } catch (error) {
                        meta = {};
                    }

                    if (Array.isArray(meta.freqList)) {
                        meta.freqList = meta.freqList.map((f) => ({
                            errorCount: f.error_count,
                            freq: f.freq,
                            len: f.len,
                            pos: f.pos,
                            spikeCount: f.spike_count,
                        }));

                    } else {
                        meta.freqList = [];
                    }

                    if (Array.isArray(meta.srcList)) {
                        meta.srcList = meta.srcList.map((s) => ({
                            src: s.src,
                            pos: s.pos,
                        }));

                    } else {
                        meta.srcList = [];
                    }

                    const data = {
                        audio: req.files.audio[0].buffer,
                        emergency: !!meta.emergency,
                        freq: parseInt(meta.freq, 10),
                        freqList: Array.isArray(meta.freqList) ? meta.freqList : [],
                        startTime: parseDate(meta.start_time),
                        stopTime: parseDate(meta.stop_time),
                        srcList: Array.isArray(meta.srcList) ? meta.srcList : [],
                        system: parseInt(req.body.system, 10),
                        talkgroup: parseInt(meta.talkgroup, 10),
                    };

                    if (!validateApiKey(key, data.system, data.talkgroup)) {
                        return res.send(`Api key is invalid for system ${data.system} talkgroup ${data.talkgroup}.\n`);
                    }

                    try {
                        const call = await models.rdioScannerCall.create(data);

                        pubsub.publish('rdioScannerCall', { rdioScannerCall: call });

                        res.send(`Call imported successfully.\n`);

                    } catch (err) {
                        res.send(err.message);
                    }
                }
            }),
        };
    }
}

module.exports = TrunkRecorderCallUpload;
