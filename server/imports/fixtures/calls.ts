import { Calls } from '../../../imports/collections/calls';

Calls.rawCollection().createIndex({ createdAt: 1 }, { name: '_createdAt_' });
Calls.rawCollection().createIndex({ startTime: 1 }, { name: '_startTime_' });
