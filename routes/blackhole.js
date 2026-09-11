const express = require('express');
const router = express.Router();
const stats = require('../middleware/blackhole/stats');
const latency = require('../middleware/blackhole/latency');
const failure = require('../middleware/blackhole/failure');
const projects = require('../middleware/blackhole/projects');

router.get('/stats', function (req, res) {
    res.json(stats.computeStats());
});

router.delete('/recent', function (req, res) {
    stats.clearRecent();
    res.sendStatus(204);
});

const PROJECT_NAME_RE = /^[a-zA-Z0-9_-]{1,64}$/;

router.post('/projects', function (req, res) {
    const {name} = req.body;
    if (!name || typeof name !== 'string' || !PROJECT_NAME_RE.test(name)) {
        return res.status(400).json({error: 'name must be 1-64 chars of letters, digits, - or _'});
    }
    const token = projects.create(name);
    if (!token) {
        return res.status(409).json({error: `project '${name}' already exists`});
    }
    res.status(201).json({name, token});
});

router.get('/projects', function (req, res) {
    res.json(projects.list());
});

function parseConfigPath(path) {
    if (!path || typeof path !== 'string' || !path.startsWith('/blackhole/')) {
        return {error: 'path must be a subpath under /blackhole/<project>/, e.g. /blackhole/my-project/my-scenario'};
    }
    const projectName = path.slice('/blackhole/'.length).split('/')[0];
    if (!projectName) {
        return {error: 'path must be a subpath under /blackhole/<project>/, e.g. /blackhole/my-project/my-scenario'};
    }
    return {projectName};
}

function authorizeConfigPath(req, path) {
    const parsed = parseConfigPath(path);
    if (parsed.error) {
        return {status: 400, error: parsed.error};
    }
    if (!projects.exists(parsed.projectName)) {
        return {
            status: 404,
            error: `project '${parsed.projectName}' does not exist -- create it first via POST /blackhole/projects`,
        };
    }
    const token = req.get('X-Blackhole-Token');
    if (!token || !projects.verify(parsed.projectName, token)) {
        return {status: 403, error: `invalid or missing token for project '${parsed.projectName}'`};
    }
    return null;
}

router.get('/config/latency', function (req, res) {
    res.json(latency.list());
});

router.post('/config/latency', function (req, res) {
    const {path, jitterMs} = req.body;
    const authError = authorizeConfigPath(req, path);
    if (authError) {
        return res.status(authError.status).json({error: authError.error});
    }
    const jitter = Number(jitterMs);
    if (!Number.isFinite(jitter) || jitter < 0) {
        return res.status(400).json({error: 'jitterMs must be a non-negative number'});
    }
    latency.set(path, jitter);
    res.status(201).json({path, jitterMs: jitter});
});

router.delete('/config/latency', function (req, res) {
    const path = req.query.path;
    if (!path) {
        return res.status(400).json({error: 'path query param is required'});
    }
    const authError = authorizeConfigPath(req, path);
    if (authError) {
        return res.status(authError.status).json({error: authError.error});
    }
    latency.remove(path);
    res.sendStatus(204);
});

router.get('/config/failure', function (req, res) {
    res.json(failure.list());
});

router.post('/config/failure', function (req, res) {
    const {path, rate, statusCode} = req.body;
    const authError = authorizeConfigPath(req, path);
    if (authError) {
        return res.status(authError.status).json({error: authError.error});
    }
    const rateNum = Number(rate);
    if (!Number.isFinite(rateNum) || rateNum < 0 || rateNum > 1) {
        return res.status(400).json({error: 'rate must be a number between 0 and 1'});
    }
    const statusCodeNum = Number(statusCode);
    if (!Number.isInteger(statusCodeNum) || statusCodeNum < 400 || statusCodeNum > 599) {
        return res.status(400).json({error: 'statusCode must be an integer between 400 and 599'});
    }
    failure.set(path, rateNum, statusCodeNum);
    res.status(201).json({path, rate: rateNum, statusCode: statusCodeNum});
});

router.delete('/config/failure', function (req, res) {
    const path = req.query.path;
    if (!path) {
        return res.status(400).json({error: 'path query param is required'});
    }
    const authError = authorizeConfigPath(req, path);
    if (authError) {
        return res.status(authError.status).json({error: authError.error});
    }
    failure.remove(path);
    res.sendStatus(204);
});

function respond(req, res) {
    const fullPath = req.originalUrl.split('?')[0];
    const failStatusCode = failure.statusCodeFor(fullPath);
    if (failStatusCode) {
        return res.status(failStatusCode).json({
            method: req.method,
            path: fullPath,
            injectedFailure: true,
            statusCode: failStatusCode,
        });
    }
    res.json({
        method: req.method,
        path: fullPath,
        url: req.protocol + '://' + req.get('host') + req.originalUrl,
        args: req.query,
        headers: req.headers,
        body: req.body,
        origin: req.socket.remoteAddress,
    });
}

function delayedRespond(req, res) {
    const fullPath = req.originalUrl.split('?')[0];
    const delayMs = latency.delayFor(fullPath);
    setTimeout(() => respond(req, res), delayMs);
}

router.all('/', delayedRespond);
router.all('/*splat', delayedRespond);

module.exports = router;
