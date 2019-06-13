import { Systems } from '../../../imports/collections/systems';

Systems.rawCollection().createIndex({ system: 1 }, { name: '_system_' });
