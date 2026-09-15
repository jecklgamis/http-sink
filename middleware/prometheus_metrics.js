const client = require('@prometheus-io/client');
const onFinished = require('on-finished');

const register = new client.Registry();
client.collectDefaultMetrics({register});

const httpRequestDuration = new client.Histogram({
    name: 'http_request_duration_seconds',
    help: 'HTTP request duration in seconds',
    labelNames: ['method', 'route', 'status_code'],
    buckets: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10],
    registers: [register],
});

const httpRequestsTotal = new client.Counter({
    name: 'http_requests_total',
    help: 'Total HTTP requests',
    labelNames: ['method', 'route', 'status_code'],
    registers: [register],
});

// bucket by top-level path segment (e.g. "/sink" for anything under /sink/*) to keep
// label cardinality bounded -- /sink accepts arbitrary echoed subpaths
function routeLabel(path) {
    if (!path || path === '/') return '/';
    const first = path.split('/')[1];
    return first ? '/' + first : '/';
}

function prometheusMetrics(req, res, next) {
    const start = process.hrtime.bigint();
    // capture method/route now -- req.path can be left mutated by a mounted sub-router
    // (e.g. /sink/*) once its route handler responds without calling next(), so reading
    // it lazily inside onFinished would see the router-relative path instead of the real one
    const method = req.method;
    const route = routeLabel(req.path);
    onFinished(res, function () {
        const durationSeconds = Number(process.hrtime.bigint() - start) / 1e9;
        const labels = {method: method, route: route, status_code: res.statusCode};
        httpRequestDuration.observe(labels, durationSeconds);
        httpRequestsTotal.inc(labels);
    });
    next();
}

module.exports = {prometheusMetrics, register};
