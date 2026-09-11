const crypto = require('crypto');

let projects = {};

function create(name) {
    if (projects[name]) {
        return null;
    }
    const token = crypto.randomBytes(24).toString('hex');
    projects[name] = {token, createdAt: Date.now()};
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

module.exports = {create, exists, verify, list};
