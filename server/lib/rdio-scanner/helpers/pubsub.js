const EventEmitter = require('events');
const { PubSubEngine } = require('apollo-server-express');

class PubSub extends PubSubEngine {
    constructor() {
        super();

        this.eventEmitter = new EventEmitter();

        this.subscriptions = {};

        this.subIdCounter = 0;
    }

    async publish(triggerName, payload) {
        this.eventEmitter.emit(triggerName, payload);

        return Promise.resolve();
    }

    async subscribe(triggerName, onMessage) {
        this.eventEmitter.addListener(triggerName, onMessage);

        this.subIdCounter = ++this.subIdCounter;

        this.subscriptions[this.subIdCounter] = [triggerName, onMessage];

        this.logLiveFeedListeners(triggerName);

        return this.subIdCounter;
    }

    async unsubscribe(subId) {
        const [triggerName, onMessage] = this.subscriptions[subId];

        delete this.subscriptions[subId];

        this.eventEmitter.removeListener(triggerName, onMessage);

        this.logLiveFeedListeners(triggerName);
    }

    logLiveFeedListeners(triggerName) {
        const livefeed = 'rdioScannerCall';

        if (triggerName === livefeed) {
            const count = Object.keys(this.subscriptions).reduce((c, k) => this.subscriptions[k][0] === livefeed ? ++c : c, 0);

            console.log(`Live feed listeners: ${count}`);
        }
    }
}

module.exports = PubSub;
