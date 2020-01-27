'use strict';

require('dotenv').config();

module.exports = () => async () => {
    let allowDownload = process.env.RDIO_ALLOW_DOWNLOAD || true;

    allowDownload = `${allowDownload}`.toLowerCase() !== 'false';

    let useGroup = process.env.RDIO_USE_GROUP || true;

    useGroup = `${useGroup}`.toLowerCase() !== 'false';

    return { allowDownload, useGroup };
};
