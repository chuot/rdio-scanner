import { check } from 'meteor/check';
import { Meteor } from 'meteor/meteor';
import { Calls } from '../../../imports/collections/calls';

Meteor.methods({ 'calls-count': callsCount });

function callsCount(selector: any = {}): number {
    check(selector, Object);

    return Calls.collection.find(selector).count();
}
