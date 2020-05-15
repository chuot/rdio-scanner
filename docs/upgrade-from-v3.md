# Upgrading from **Rdio Scanner v3**

Everything has been put in place to ensure a smooth transition to version 4.0.

Important notes:

1) Your `server/.env` file will be use to create the new `server/config.json` file. Then the `server/.env` will be deleted.

2) The `rdioScannerSystems` table will be used to create the *rdioScanner.systems* withing `server/config.json`. Then the `rdioScannerSystems` table will be purged.

3) The `rdioScannerCalls` table will be rebuilt, which can be pretty long on some systems.

4) It is no longer possible to upload neither your TALKGROUP.CSV nor you ALIAS.CSV files to **Rdio Scanner**. Instead, you have to define them in the `server/config.json` file.

If something is going wrong while updating to **Rdio Scanner v4.0**, remember to [![Chat](https://img.shields.io/gitter/room/rdio-scanner/Lobby.svg)](https://gitter.im/rdio-scanner/Lobby?utm_source=share-link&utm_medium=link&utm_campaign=share-link) for advices.

> YOU SHOULD REALLY BACKUP YOUR `SERVER/.ENV` FILE AND YOUR DATABASE PRIOR TO UPGRADING, JUST IN CASE. WE'VE TESTED THE UPGRADE PROCESS MANY TIMES, BUT WE CAN'T KNOW FOR SURE IF IT'S GOING TO WORK WELL ON YOUR SIDE.