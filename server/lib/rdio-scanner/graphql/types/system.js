'use strict';

const schema = `
    type RdioScannerSystem {
        aliases: [RdioScannerAlias]
        id: String
        name: String
        system: Int
        talkgroups: [RdioScannerTalkgroup]
    }
`;

module.exports = { schema };
