let configs = {};

function set(path, rate, statusCode) {
    configs[path] = {rate, statusCode};
}

function remove(path) {
    delete configs[path];
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
