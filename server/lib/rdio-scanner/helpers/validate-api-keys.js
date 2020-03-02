'use strict';

const getApiKeys = require('./get-api-key');

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

module.exports = validateApiKey;
