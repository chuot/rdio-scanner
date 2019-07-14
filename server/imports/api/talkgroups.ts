import { IncomingMessage, ServerResponse } from 'http';
import { Meteor } from 'meteor/meteor';
import { WebApp } from 'meteor/webapp';
import { Form } from 'multiparty';
import { Systems } from '../../../imports/collections/systems';
import { System } from '../../../imports/models/system';
import { Talkgroup } from '../../../imports/models/talkgroup';

if (Meteor.isServer) {
    createApiHandler();
}

function createApiHandler(): void {
    WebApp.connectHandlers.use('/talkgroups', (req: IncomingMessage, res: ServerResponse) => {
        if (req.method === 'POST') {
            const form = new Form();

            let csv = '';
            let key = '';
            let name = '';
            let system = '';

            form.on('part', (part) => {
                part.on('data', (data) => {
                    switch (part.name.toLowerCase()) {
                        case 'csv':
                            csv += data.toString('utf8');
                            break;
                        case 'key':
                            key += data.toString('utf8');
                            break;
                        case 'name':
                            name += data.toString('utf8');
                            break;
                        case 'system':
                            system += data.toString('utf8');
                            break;
                    }
                });
                part.resume();
            });

            form.on('close', Meteor.bindEnvironment(() => {
                const apiKeys = Meteor.settings.apiKeys;

                if (key && Array.isArray(apiKeys) && apiKeys.find((apiKey) => apiKey === key)) {
                    const sys = new System({
                        name,
                        system,
                        talkgroups: csv
                            .replace('\r', '')
                            .split('\n')
                            .filter((line: string) => line.charAt(0) !== '#')
                            .map((line: string) => {
                                const fields = line.split(',');
                                return ['dec', 'hex', 'mode', 'alphaTag', 'description', 'tag', 'group', 'priority']
                                    .reduce((tg, k, i) => {
                                        tg[k] = fields[i];
                                        return tg;
                                    }, {});
                            })
                            .filter((talkgroup: Talkgroup) => talkgroup.dec),
                    });

                    if (sys.talkgroups.length) {
                        Systems.collection.update({ system: sys.system }, { $set: sys }, { upsert: true }, (error: Meteor.Error) => {
                            res.writeHead(error ? 500 : 200);
                            res.end();
                        });

                    } else {
                        Systems.collection.remove({ system: sys.system }, (error: Meteor.Error) => {
                            res.writeHead(error ? 500 : 200);
                            res.end();
                        });
                    }
                } else {
                    res.writeHead(403);
                    res.end('wrong or no api key provided\n');
                }
            }));

            form.parse(req);

        } else {
            res.writeHead(404);
            res.end();
        }
    });
}
