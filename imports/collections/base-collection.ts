import { Mongo } from 'meteor/mongo';

export abstract class BaseCollection<T> extends Mongo.Collection<T> {
    insert(document: any = {}, cb: () => void): string {
        document.createdAt = document.modifiedAt = new Date();
        return super.insert(document, cb);
    }

    update(selector: any = {}, modifier: any = {}, options: any, cb: () => void): number {
        modifier.$set = modifier.$set || {};
        modifier.$set.modifiedAt = new Date();
        return super.update(selector, modifier, options, cb);
    }

    upsert(selector: any = {}, modifier: any = {}, options: any, cb: () => void): { numberAffected?: number, affectedId?: string } {
        modifier.$setOnInsert = modifier.$setOnInsert || {};
        modifier.$setOnInsert.createdAt = new Date();
        modifier.$set = modifier.$set || {};
        modifier.$set.modifiedAt = new Date();
        return super.upsert(selector, modifier, options, cb);
    }
}
