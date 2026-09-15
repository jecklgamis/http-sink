const fs = require('fs');
const os = require('os');
const path = require('path');

// tests import app.js directly, which loads middleware/sink/store.js at require-time --
// point it at an isolated temp dir before that happens, so `npm test` never reads/writes
// the real ./data used by a locally running dev server
const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'http-sink-test-data-'));
process.env.SINK_DATA_DIR = tmpDir;

process.on('exit', function () {
    fs.rmSync(tmpDir, {recursive: true, force: true});
});
