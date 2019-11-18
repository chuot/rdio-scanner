'use strict';

const fs = require('fs');
const path = require('path');

const mainFile = 'main.html';

module.exports = ({ clientRoot }) => (req, res) => {
    if (fs.existsSync(path.join(clientRoot, mainFile))) {
        return res.sendFile(mainFile, { root: clientRoot });
    } else {
        return res.send('A new build of Rdio Scanner is being prepared. Please check back in a few minutes.');
    }
};
