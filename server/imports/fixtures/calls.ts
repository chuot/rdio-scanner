import { Calls } from '../../../imports/collections/calls';

Calls.rawCollection().createIndex({ createdAt: 1 }, { name: '_createdAt_' });
Calls.rawCollection().createIndex({ startTime: 1 }, { name: '_startTimeAsc_' });
Calls.rawCollection().createIndex({ system: 1 }, { name: '_system_' });
Calls.rawCollection().createIndex({ talkgroup: 1 }, { name: '_talkgroup_' });
