import { Meteor } from 'meteor/meteor';
import { Systems } from '../../../imports/collections/systems';
import { System } from '../../../imports/models/system';

Meteor.publish('systems', (): Mongo.Cursor<System> => {
    return Systems.collection.find();
});
