import { check } from 'meteor/check';
import { Meteor } from 'meteor/meteor';
import { Calls } from '../../../imports/collections/calls';
import { Call } from '../../../imports/models/call';

Meteor.publish('calls', function calls(selector: any = {}, options: any = {}): Mongo.Cursor<Call> {
    check (selector, Object);
    check (options, Object);

    return Calls.collection.find(selector, options);
});
