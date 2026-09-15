const fs = require('fs');
const path = require('path');

// Single swap point for how sink config (projects/latency/failure) is persisted.
// Today this is a JSON file per store, loaded once at startup and rewritten on every
// mutation. Callers (projects.js/latency.js/failure.js) only ever see a plain object
// and a persist() call -- swapping this for Redis/etcd later means changing this file
// only, not the call sites, though a networked backend would make persist()/load() async.
const DATA_DIR = process.env.SINK_DATA_DIR || path.join(__dirname, '..', '..', 'data');

function filePathFor(name) {
    return path.join(DATA_DIR, `${name}.json`);
}

function load(name) {
    try {
        return JSON.parse(fs.readFileSync(filePathFor(name), 'utf8'));
    } catch (e) {
        return {};
    }
}

function save(name, data) {
    fs.mkdirSync(DATA_DIR, {recursive: true});
    const file = filePathFor(name);
    const tmpFile = `${file}.${process.pid}.tmp`;
    fs.writeFileSync(tmpFile, JSON.stringify(data));
    fs.renameSync(tmpFile, file);
}

// Returns a live plain object pre-populated from disk, plus a persist() call
// that writes its current contents back out. Callers mutate `data` directly.
function createStore(name) {
    const data = load(name);
    return {
        data,
        persist() {
            save(name, data);
        },
    };
}

module.exports = {createStore};
