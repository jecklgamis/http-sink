const {createStore} = require('./store');

const store = createStore('failure');
const configs = store.data;

function set(path, rate, statusCode) {
    configs[path] = {rate, statusCode};
    store.persist();
}

function remove(path) {
    delete configs[path];
    store.persist();
}

function list() {
    return Object.keys(configs).map(path => ({path, rate: configs[path].rate, statusCode: configs[path].statusCode}));
}

function statusCodeFor(path) {
    const config = configs[path];
    if (!config) return null;
    return Math.random() < config.rate ? config.statusCode : null;
}

module.exports = {set, remove, list, statusCodeFor};
