const crypto = require('crypto');
const {createStore} = require('./store');

const store = createStore('projects');
const projects = store.data;

function create(name) {
    if (projects[name]) {
        return null;
    }
    const token = crypto.randomBytes(24).toString('hex');
    projects[name] = {token, createdAt: Date.now()};
    store.persist();
    return token;
}

function exists(name) {
    return !!projects[name];
}

function verify(name, token) {
    return !!projects[name] && projects[name].token === token;
}

function list() {
    return Object.keys(projects).map(name => ({name, createdAt: projects[name].createdAt}));
}

function remove(name) {
    delete projects[name];
    store.persist();
}

module.exports = {create, exists, verify, list, remove};
