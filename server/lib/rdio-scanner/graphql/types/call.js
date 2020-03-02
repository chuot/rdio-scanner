'use strict';

const schema = `
    type RdioScannerCall {
        id: String
        audio: RdioScannerAudio
        audioName: String
        audioType: String
        emergency: Boolean
        freq: Int
        freqList: [RdioScannerCallFreq]
        startTime: RdioScannerDate
        stopTime: RdioScannerDate
        srcList: [RdioScannerCallSrc]
        system: Int
        talkgroup: Int
    }
`;

module.exports = { schema };
