const express = require('express');
const router = express.Router();
const stats = require('../middleware/blackhole/stats');
const latency = require('../middleware/blackhole/latency');
const failure = require('../middleware/blackhole/failure');

router.get('/stats', function (req, res) {
    res.json(stats.computeStats());
});

router.delete('/recent', function (req, res) {
    stats.clearRecent();
    res.sendStatus(204);
});

router.get('/config/latency', function (req, res) {
    res.json(latency.list());
});

router.post('/config/latency', function (req, res) {
    const {path, jitterMs} = req.body;
    if (!path || typeof path !== 'string' || !path.startsWith('/')) {
        return res.status(400).json({error: 'path must be a string starting with /'});
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
    latency.remove(path);
    res.sendStatus(204);
});

router.get('/config/failure', function (req, res) {
    res.json(failure.list());
});

router.post('/config/failure', function (req, res) {
    const {path, rate, statusCode} = req.body;
    if (!path || typeof path !== 'string' || !path.startsWith('/')) {
        return res.status(400).json({error: 'path must be a string starting with /'});
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
