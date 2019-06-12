import { MongoObservable } from 'meteor-rxjs';
import { System } from '../models/system';
import { BaseCollection } from './base-collection';

class Collection extends BaseCollection<System> { }

export const Systems = new MongoObservable.Collection<System>(new Collection('systems'));
