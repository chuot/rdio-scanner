import { IncomingMessage, ServerResponse } from 'http';
import { Meteor } from 'meteor/meteor';
import { WebApp } from 'meteor/webapp';
import { Form } from 'multiparty';
import { Calls } from '../../../imports/collections/calls';
import { Call } from '../../../imports/models/call';

if (Meteor.isServer) {
    createApiHandler();
    createPruneCallsTimer();
}

function base64Encode(value: any): string {
    return Buffer.from(value, 'binary').toString('base64');
}

function createApiHandler(): void {
    WebApp.connectHandlers.use('/upload', (req: IncomingMessage, res: ServerResponse) => {
        if (req.method === 'POST') {
            const form = new Form();

            let audio = '';
            let json = '';
            let key = '';
            let mimeType = '';
            let system = '';

            form.on('part', (part) => {
                part.on('data', (data) => {
                    switch (part.name.toLowerCase()) {
                        case 'audio':
                            audio += data.toString('binary');
                            mimeType = part.headers['content-type'];
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
                    try {
                        const call = new Call(Object.assign({}, JSON.parse(json), {
                            audio: urlEncode(mimeType, audio),
                            system,
                        }));

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

function createPruneCallsTimer(): void {
    Meteor.setInterval(() => pruneCalls(), 10000);
}

function getPruneDays(): number | null {
    const pruneDays = Meteor.settings.pruneDays;
    return typeof pruneDays === 'number' ? pruneDays : null;
}

function pruneCalls(): void {
    const pruneDays = getPruneDays();

    if (pruneDays !== null) {
        const currentDate = new Date();
        const dateLimit = new Date(currentDate.getFullYear(), currentDate.getMonth(), currentDate.getDate() - pruneDays);

        Calls.collection.remove({
            createdAt: {
                $lt: dateLimit,
            },
        });
    }
}

function urlEncode(mimeType: string, value: any): string {
    return `data:${mimeType};base64,${base64Encode(value)}`;
}
