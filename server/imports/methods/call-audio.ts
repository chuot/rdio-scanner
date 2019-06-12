import { check } from 'meteor/check';
import { Meteor } from 'meteor/meteor';
import { Calls } from '../../../imports/collections/calls';

Meteor.methods({ 'call-audio': callAudio });

function callAudio(id: string): string {
    check(id, String);

    const call = Calls.collection.findOne({ _id: id });

    return call && call.audio;
}
