import { MongoObservable } from 'meteor-rxjs';
import { Call } from '../models/call';
import { BaseCollection } from './base-collection';

class Collection extends BaseCollection<Call> { }

export const Calls = new MongoObservable.Collection<Call>(new Collection('calls'));
