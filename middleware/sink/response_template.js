const {createStore} = require('./store');

const store = createStore('response-template');
const configs = store.data;

const ANY_METHOD = 'ANY';

function keyFor(path, method) {
    return `${(method || ANY_METHOD).toUpperCase()}:${path}`;
}

function set(path, statusCode, headers, body, method) {
    const normalizedMethod = (method || ANY_METHOD).toUpperCase();
    configs[keyFor(path, normalizedMethod)] = {path, method: normalizedMethod, statusCode, headers, body};
    store.persist();
}

function remove(path, method) {
    delete configs[keyFor(path, method)];
    store.persist();
}

function list() {
    return Object.keys(configs).map(key => {
        const c = configs[key];
        return {
            path: c.path,
            method: c.method === ANY_METHOD ? undefined : c.method,
            statusCode: c.statusCode,
            headers: c.headers,
            body: c.body,
        };
    });
}

// exact method match takes precedence over a method-agnostic (ANY) template on the same path
function templateFor(path, method) {
    return configs[keyFor(path, method)] || configs[keyFor(path, ANY_METHOD)] || null;
}

module.exports = {set, remove, list, templateFor};
