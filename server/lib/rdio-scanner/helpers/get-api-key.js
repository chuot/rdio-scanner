'use strict';

function getApiKeys() {
    let apiKeys = ['b29eb8b9-9bcd-4e6e-bb4f-d244ada12736'];

    if (process.env.RDIO_APIKEYS) {
        try {
            apiKeys = JSON.parse(process.env.RDIO_APIKEYS);
        } catch (err) {
            console.log('ERROR: Invalid JSON format in RDIO_APIKEYS.');
        }
    }

    return apiKeys;
}

module.exports = getApiKeys;
