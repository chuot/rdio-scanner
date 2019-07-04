import { IncomingMessage, ServerResponse } from 'http';
import { Meteor } from 'meteor/meteor';
import { WebApp } from 'meteor/webapp';
import { Form } from 'multiparty';
import { Calls } from '../../../imports/collections/calls';
import { Call } from '../../../imports/models/call';

if (Meteor.isServer) {
    WebApp.connectHandlers.use('/upload', (req: IncomingMessage, res: ServerResponse) => {
        if (req.method === 'POST') {
            const form = new Form();

            let audio = '';
            let json = '';
            let key = '';
            let system = '';

            form.on('part', (part) => {
                part.on('data', (data) => {
                    switch (part.name.toLowerCase()) {
                        case 'audio':
                            audio += data.toString('binary');
                            break;
                        case 'json':
                            json += data.toString('utf8');
                            break;
                        case 'key':
                            key += data.toString('utf8');
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
                    audio = 'data:audio/mpeg;base64,' + Buffer.from(audio, 'binary').toString('base64');

                    try {
                        const call = new Call(Object.assign({}, JSON.parse(json), { audio, system }));

                        Calls.collection.insert(call, (error: Meteor.Error) => {
                            res.writeHead(error ? 500 : 200);
                            res.end();
                        });

                    } catch (error) {
                        res.writeHead(500);
                        res.end(error.message);
                    }

                } else {
                    res.writeHead(403);
                    res.end('wrong or no api key provided');
                }
            }));

            form.parse(req);

        } else {
            res.writeHead(404);
            res.end();
        }
    });
}
