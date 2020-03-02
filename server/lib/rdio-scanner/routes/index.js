'use strict';

const TrunkRecorderCallUpload = require('./trunk-recorder-call-upload');
const TrunkRecorderSystemUpload = require('./trunk-recorder-system-upload');

class Routes {
    constructor(models, pubsub, router) {
        this.trunkRecorderCallUpload = new TrunkRecorderCallUpload(models, pubsub);
        this.trunkRecorderSystemUpload = new TrunkRecorderSystemUpload(models, pubsub);

        router.use(this.trunkRecorderCallUpload.path, this.trunkRecorderCallUpload.middleware);
        router.use(this.trunkRecorderSystemUpload.path, this.trunkRecorderSystemUpload.middleware);
    }
}

module.exports = Routes;
