'use strict';

require('dotenv').config();

const cors = require('cors');
const express = require('express');
const fs = require('fs');
const helmet = require('helmet');
const http = require('http');
const path = require('path');

const RdioScanner = require('./lib/rdio-scanner');

const env = process.env.NODE_ENV || 'development';
const host = process.env.NODE_HOST || '0.0.0.0';
const staticDir = path.resolve(process.env.APP_CLIENT_DIR || '../client/dist');
const staticFile = process.env.APP_CLIENT_FILE || 'main.html';
const port = parseInt(process.env.NODE_PORT, 10) || 3000;

class App {
    constructor() {
        this.router = express();
        this.router.use(express.json());
        this.router.use(express.urlencoded({ extended: false }));
        this.router.use(express.static(staticDir));
        this.router.set(port);

        if (env !== 'development') {
            this.router.disable('x-powered-by');

            this.router.get(/\/|\/index.html/, cors(), (req, res) => {
                if (fs.existsSync(path.join(staticDir, staticFile))) {
                    return res.sendFile(staticFile, { root: staticDir });

                } else {
                    return res.send('A new build is being prepared. Please check back in a few minutes.');
                }
            });

            this.router.use(helmet());
        }

        this.httpServer = http.createServer(this.router);
        this.httpServer.listen(port, host, () => console.log(`Server is running at http://${host}:${port}/`));

        this.rdioScanner = new RdioScanner(this.httpServer, this.router);
    }
}

module.exports = new App();
