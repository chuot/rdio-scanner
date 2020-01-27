'use strict';

const multer = require('multer');
const upload = multer({ storage: multer.memoryStorage() });

const { callReducer, validateApiKey } = require('../helpers/rdio-scanner');

module.exports = ({ pubsub, store }) => (req, res) => upload.fields([
    { name: 'audio', maxCount: 1 },
    { name: 'key', maxCount: 1 },
    { name: 'meta', maxCount: 1 },
    { name: 'system', maxCount: 1 },
])(req, res, (err) => {
    if (err instanceof multer.MulterError) {
        res.send(`${err.message}: ${err.field}\n`);

    } else if (err) {
        res.send(`${err.message}\n`);

    } else {
        const key = req.body.key;

        let meta = req.files.meta[0].buffer.toString();

        try {
            meta = JSON.parse(meta);
        } catch (err) {
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
            audioName: req.files.audio[0].originalname,
            audioType: req.files.audio[0].mimetype,
            emergency: !!meta.emergency,
            freq: parseInt(meta.freq, 10),
            freqList: JSON.stringify(Array.isArray(meta.freqList) ? meta.freqList : []),
            startTime: parseDate(meta.start_time),
            stopTime: parseDate(meta.stop_time),
            srcList: JSON.stringify(Array.isArray(meta.srcList) ? meta.srcList : []),
            system: parseInt(req.body.system, 10),
            talkgroup: parseInt(meta.talkgroup, 10),
        };

        if (!validateApiKey(key, data.system, data.talkgroup)) {
            return res.send(`Api key is invalid for system ${data.system} talkgroup ${data.talkgroup}.\n`);
        }

        store.rdioScannerCall.create(data)
            .then((call) => {
                pubsub.publish('rdioScannerCall', { rdioScannerCall: callReducer(call) });
                res.send(`Call imported successfully.\n`)
            })
            .catch((err) => res.send(err.message));
    }
});

function parseDate(value) {
    if (value instanceof Date) {
        return value;
    } else if (typeof value === 'number') {
        const date = new Date(1970, 0, 1);
        date.setUTCSeconds(value - date.getTimezoneOffset() * 60);
        return date;
    } else {
        return new Date(value);
    }
}
