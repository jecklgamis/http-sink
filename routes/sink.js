const crypto = require('crypto');
const express = require('express');
const router = express.Router();
const stats = require('../middleware/sink/stats');
const latency = require('../middleware/sink/latency');
const failure = require('../middleware/sink/failure');
const responseTemplate = require('../middleware/sink/response_template');
const projects = require('../middleware/sink/projects');
const {toIpv4} = require('../middleware/sink/ip');

router.get('/stats', function (req, res) {
    res.json(stats.computeStats());
});

router.delete('/recent', function (req, res) {
    stats.clearRecent();
    res.sendStatus(204);
});

const PROJECT_NAME_RE = /^[a-zA-Z0-9_-]{1,64}$/;

// fixed top-level /sink/<segment> routes -- a project name (and therefore any latency/failure
// config path) can never shadow one of these, so the sink utilities are always unaffected by
// latency/failure injection, which only applies to the generic echo catch-all
const RESERVED_SINK_SEGMENTS = [
    'stats', 'recent', 'projects', 'config',
    'headers', 'ip', 'user-agent', 'uuid', 'status', 'delay', 'redirect', 'response-headers', 'stream', 'bytes',
];

router.post('/projects', function (req, res) {
    const {name} = req.body;
    if (!name || typeof name !== 'string' || !PROJECT_NAME_RE.test(name)) {
        return res.status(400).json({error: 'name must be 1-64 chars of letters, digits, - or _'});
    }
    if (RESERVED_SINK_SEGMENTS.includes(name)) {
        return res.status(400).json({error: `'${name}' is a reserved /sink path and can't be used as a project name`});
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

function authorizeProjectToken(req, projectName) {
    if (!projects.exists(projectName)) {
        return {
            status: 404,
            error: `project '${projectName}' does not exist -- create it first via POST /sink/projects`,
        };
    }
    const token = req.get('X-Sink-Token');
    if (!token || !projects.verify(projectName, token)) {
        return {status: 403, error: `invalid or missing token for project '${projectName}'`};
    }
    return null;
}

router.delete('/projects/:name', function (req, res) {
    const name = req.params.name;
    const authError = authorizeProjectToken(req, name);
    if (authError) {
        return res.status(authError.status).json({error: authError.error});
    }
    const prefix = `/sink/${name}`;
    const underProject = p => p === prefix || p.startsWith(prefix + '/');
    latency.list().filter(c => underProject(c.path)).forEach(c => latency.remove(c.path));
    failure.list().filter(c => underProject(c.path)).forEach(c => failure.remove(c.path));
    responseTemplate.list().filter(c => underProject(c.path)).forEach(c => responseTemplate.remove(c.path, c.method));
    projects.remove(name);
    res.sendStatus(204);
});

function parseConfigPath(path) {
    if (!path || typeof path !== 'string' || !path.startsWith('/sink/')) {
        return {error: 'path must be a subpath under /sink/<project>/, e.g. /sink/my-project/my-scenario'};
    }
    const projectName = path.slice('/sink/'.length).split('/')[0];
    if (!projectName) {
        return {error: 'path must be a subpath under /sink/<project>/, e.g. /sink/my-project/my-scenario'};
    }
    if (RESERVED_SINK_SEGMENTS.includes(projectName)) {
        return {error: `'${projectName}' is a reserved /sink path -- the sink utilities can't have latency/failure configs`};
    }
    return {projectName};
}

function authorizeConfigPath(req, path) {
    const parsed = parseConfigPath(path);
    if (parsed.error) {
        return {status: 400, error: parsed.error};
    }
    return authorizeProjectToken(req, parsed.projectName);
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

const MAX_TEMPLATE_HEADER_COUNT = 20;
const MAX_TEMPLATE_BODY_BYTES = 16 * 1024;
const ALLOWED_TEMPLATE_METHODS = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'];

function validateTemplateMethod(method) {
    if (method === undefined || method === null || method === '') return {method: undefined};
    if (typeof method !== 'string' || !ALLOWED_TEMPLATE_METHODS.includes(method.toUpperCase())) {
        return {error: `method must be one of: ${ALLOWED_TEMPLATE_METHODS.join(', ')} (or omitted for any method)`};
    }
    return {method: method.toUpperCase()};
}

function validateTemplateHeaders(headers) {
    if (headers === undefined) return {headers: undefined};
    if (typeof headers !== 'object' || headers === null || Array.isArray(headers)) {
        return {error: 'headers must be an object of header name to string value'};
    }
    const entries = Object.entries(headers);
    if (entries.length > MAX_TEMPLATE_HEADER_COUNT) {
        return {error: `headers can have at most ${MAX_TEMPLATE_HEADER_COUNT} entries`};
    }
    const normalized = {};
    for (const [key, value] of entries) {
        if (typeof value !== 'string' && typeof value !== 'number' && typeof value !== 'boolean') {
            return {error: `header '${key}' value must be a string, number, or boolean`};
        }
        normalized[key] = String(value);
    }
    return {headers: normalized};
}

router.get('/config/response', function (req, res) {
    res.json(responseTemplate.list());
});

router.post('/config/response', function (req, res) {
    const {path, statusCode, headers, body, method} = req.body;
    const authError = authorizeConfigPath(req, path);
    if (authError) {
        return res.status(authError.status).json({error: authError.error});
    }
    const statusCodeNum = Number(statusCode);
    if (!Number.isInteger(statusCodeNum) || statusCodeNum < 100 || statusCodeNum > 599) {
        return res.status(400).json({error: 'statusCode must be an integer between 100 and 599'});
    }
    const parsedMethod = validateTemplateMethod(method);
    if (parsedMethod.error) {
        return res.status(400).json({error: parsedMethod.error});
    }
    const parsedHeaders = validateTemplateHeaders(headers);
    if (parsedHeaders.error) {
        return res.status(400).json({error: parsedHeaders.error});
    }
    if (body !== undefined && JSON.stringify(body).length > MAX_TEMPLATE_BODY_BYTES) {
        return res.status(400).json({error: `body must be at most ${MAX_TEMPLATE_BODY_BYTES} bytes of JSON`});
    }
    responseTemplate.set(path, statusCodeNum, parsedHeaders.headers, body, parsedMethod.method);
    res.status(201).json({path, method: parsedMethod.method, statusCode: statusCodeNum, headers: parsedHeaders.headers, body});
});

router.delete('/config/response', function (req, res) {
    const path = req.query.path;
    if (!path) {
        return res.status(400).json({error: 'path query param is required'});
    }
    const authError = authorizeConfigPath(req, path);
    if (authError) {
        return res.status(authError.status).json({error: authError.error});
    }
    const parsedMethod = validateTemplateMethod(req.query.method);
    if (parsedMethod.error) {
        return res.status(400).json({error: parsedMethod.error});
    }
    responseTemplate.remove(path, parsedMethod.method);
    res.sendStatus(204);
});

router.get('/headers', function (req, res) {
    res.json({headers: req.headers});
});

router.get('/ip', function (req, res) {
    res.json({origin: toIpv4(req.ip)});
});

router.get('/user-agent', function (req, res) {
    res.json({'user-agent': req.get('user-agent') || ''});
});

router.get('/uuid', function (req, res) {
    res.json({uuid: crypto.randomUUID()});
});

router.get('/status/:code', function (req, res) {
    const code = Number(req.params.code);
    if (!Number.isInteger(code) || code < 100 || code > 599) {
        return res.status(400).json({error: 'code must be an integer between 100 and 599'});
    }
    res.sendStatus(code);
});

const MAX_DELAY_SECONDS = 10;

router.get('/delay/:seconds', function (req, res) {
    const seconds = Number(req.params.seconds);
    if (!Number.isFinite(seconds) || seconds < 0) {
        return res.status(400).json({error: 'seconds must be a non-negative number'});
    }
    const delayMs = Math.min(seconds, MAX_DELAY_SECONDS) * 1000;
    setTimeout(() => {
        res.json({
            method: req.method,
            path: req.path,
            args: req.query,
            headers: req.headers,
            delayedSeconds: delayMs / 1000,
        });
    }, delayMs);
});

const MAX_REDIRECTS = 20;

router.get('/redirect/:n', function (req, res) {
    const n = Number(req.params.n);
    if (!Number.isInteger(n) || n < 0) {
        return res.status(400).json({error: 'n must be a non-negative integer'});
    }
    if (n === 0) {
        return respond(req, res);
    }
    const remaining = Math.min(n, MAX_REDIRECTS) - 1;
    res.redirect(302, remaining > 0 ? `/sink/redirect/${remaining}` : '/sink');
});

router.get('/response-headers', function (req, res) {
    Object.entries(req.query).forEach(([key, value]) => {
        res.set(key, String(value));
    });
    res.json(req.query);
});

const MAX_STREAM_LINES = 100;

router.get('/stream/:n', function (req, res) {
    const n = Number(req.params.n);
    if (!Number.isInteger(n) || n < 0) {
        return res.status(400).json({error: 'n must be a non-negative integer'});
    }
    const count = Math.min(n, MAX_STREAM_LINES);
    res.set('Content-Type', 'text/plain');
    for (let i = 0; i < count; i++) {
        res.write(JSON.stringify({id: i, headers: req.headers, url: req.originalUrl}) + '\n');
    }
    res.end();
});

const MAX_BYTES = 100 * 1024;

router.get('/bytes/:n', function (req, res) {
    const n = Number(req.params.n);
    if (!Number.isInteger(n) || n < 0) {
        return res.status(400).json({error: 'n must be a non-negative integer'});
    }
    const size = Math.min(n, MAX_BYTES);
    res.set('Content-Type', 'application/octet-stream');
    res.send(crypto.randomBytes(size));
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
    const template = responseTemplate.templateFor(fullPath, req.method);
    if (template) {
        if (template.headers) res.set(template.headers);
        return res.status(template.statusCode).json(template.body);
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
