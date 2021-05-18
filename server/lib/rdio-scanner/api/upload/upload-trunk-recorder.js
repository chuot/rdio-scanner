/*
 * *****************************************************************************
 * Copyright (C) 2019-2021 Chrystian Huot <chrystian.huot@saubeo.solutions>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>
 * ****************************************************************************
 */

'use strict';

import multer from 'multer';

const upload = multer({ storage: multer.memoryStorage() });

export class UploadTrunkRecorder {
    constructor (ctx) {
        this.path = '/api/trunk-recorder-call-upload';

        this.controller = ctx.controller;

        ctx.router.delete(this.path, this.delete());
        ctx.router.get(this.path, this.get());
        ctx.router.patch(this.path, this.patch());
        ctx.router.post(this.path, this.post());
        ctx.router.put(this.path, this.put());
    }

    delete() {
        return (req, res) => {
            res.sendStatus(405);
        };
    }

    get() {
        return (req, res) => {
            res.sendStatus(405);
        };
    }

    patch() {
        return (req, res) => {
            res.sendStatus(405);
        };
    }

    post() {
        return (req, res) => upload.fields([
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
                const apiKey = req.body.key;

                const reqAudio = req.files.audio[0] || {};
                const reqMeta = req.files.meta[0] || {};

                const audio = reqAudio.buffer;
                const audioName = reqAudio.originalname;
                const audioType = reqAudio.mimetype;

                const system = parseInt(req.body.system, 10);

                let meta;

                try {
                    meta = JSON.parse(reqMeta.buffer.toString());

                } catch (_) {
                    meta = {};
                }

                if (!this.controller.validateApiKey(apiKey, system, meta.talkgroup)) {
                    return res.status(403).send(`Invalid API key for system ${system} talkgroup ${meta.talkgroup}.\n`);
                }

                try {
                    await this.controller.importTrunkRecorder(audio, audioName, audioType, system, meta);

                    res.send('Call imported successfully.\n');

                } catch (error) {
                    res.send(error.message);
                }
            }
        });
    }

    put() {
        return (req, res) => {
            res.sendStatus(405);
        };
    }
}
