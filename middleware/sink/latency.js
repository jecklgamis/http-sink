const {createStore} = require('./store');

const store = createStore('latency');
const configs = store.data;

function set(path, jitterMs) {
    configs[path] = jitterMs;
    store.persist();
}

function remove(path) {
    delete configs[path];
    store.persist();
}

function list() {
    return Object.keys(configs).map(path => ({path, jitterMs: configs[path]}));
}

function delayFor(path) {
    const jitterMs = configs[path];
    if (!jitterMs) return 0;
    return Math.floor(Math.random() * jitterMs);
}

module.exports = {set, remove, list, delayFor};
