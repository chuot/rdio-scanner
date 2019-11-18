'use strict';

function callReducer(call) {
    if (Array.isArray(call)) {
        return call.map((c) => callReducer(c));

    } else {
        try {
            call.freqList = JSON.parse(call.freqList);
        } catch (err) {
            call.freqList = [];
        }

        try {
            call.srcList = JSON.parse(call.srcList);
        } catch (err) {
            call.srcList = [];
        }

        return call;
    }
}

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

function pruningScheduler({ store }) {
    let pruneDays = parseInt(process.env.RDIO_PRUNEDAYS, 10);

    pruneDays = isNaN(pruneDays) ? 7 : pruneDays;

    if (store === null || typeof store !== 'object' || pruneDays < 1) {
        return null;
    }

    const Op = store.Sequelize && store.Sequelize.Op || {};

    return setInterval(() => {
        const now = new Date();

        store.rdioScannerCall.destroy({
            where: {
                startTime: {
                    [Op.lt]: new Date(now.getFullYear(), now.getMonth(), now.getDate() - pruneDays),
                },
            },
        });
    }, 15 * 60 * 1000);
}

function systemReducer(system) {
    if (Array.isArray(system)) {
        return system.map((s) => systemReducer(s));

    } else {
        try {
            system.talkgroups = JSON.parse(system.talkgroups);
        } catch (err) {
            system.talkgroups = [];
        }

        return system;
    }
}

function validateApiKey(key, system, talkgroup) {
    const apiKeys = getApiKeys();

    if (typeof apiKeys === 'string') {
        return apiKeys === key;
    }

    if (Array.isArray(apiKeys)) {
        return apiKeys.some((apiKey) => {
            if (typeof apiKey === 'string') {
                return apiKey === key;
            }

            if (apiKey !== null && typeof apiKey === 'object') {
                return apiKey.key === key && Array.isArray(apiKey.systems) && apiKey.systems.some((sys) => {
                    if (typeof sys === 'number') {
                        return sys === system;
                    }

                    if (sys !== null && typeof sys === 'object') {
                        if (typeof talkgroup === 'undefined') {
                            return sys.system === +system;
                        }

                        return sys.system === +system && Array.isArray(sys.talkgroups) && sys.talkgroups.includes(+talkgroup);
                    }

                    return false;
                });
            }

            return false;
        });
    } else {
        return false;
    }
}

module.exports = { callReducer, pruningScheduler, systemReducer, validateApiKey };
