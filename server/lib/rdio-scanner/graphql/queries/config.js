'use strict';

const schema = `
    rdioScannerConfig: RdioScannerConfig
`;

function resolver() {
    return {
        async rdioScannerConfig() {
            let allowDownload = process.env.RDIO_ALLOW_DOWNLOAD || true;

            allowDownload = `${allowDownload}`.toLowerCase() !== 'false';

            let useGroup = process.env.RDIO_USE_GROUP || true;

            useGroup = `${useGroup}`.toLowerCase() !== 'false';

            return { allowDownload, useGroup };
        },
    };
}

module.exports = { resolver, schema };
