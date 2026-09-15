const createError = require('http-errors');
const express = require('express');
const path = require('path');
const cookieParser = require('cookie-parser');
const logger = require('morgan');

const indexRouter = require('./routes/index');
const buildInfoRouter = require('./routes/build_info.js');
const sinkRouter = require('./routes/sink.js');
const liveProbeRouter = require('./routes/live_probe.js');
const readyProbeRouter = require('./routes/ready_probe.js');

const fs = require('fs');
const privateKey = fs.readFileSync('server.key', 'utf8');
const certificate = fs.readFileSync('server.crt', 'utf8');
const credentials = {key: privateKey, cert: certificate};
const app = express();

app.set('views', path.join(__dirname, 'views'));
app.set('view engine', 'pug');

// a sink/mock server must never return a cached 304 for res.json()/res.send() responses --
// every request should reflect current state, not the client's last-seen ETag
app.set('etag', false);

// behind nginx-ingress: trust X-Forwarded-For so req.ip is the real client, not the ingress pod
app.set('trust proxy', true);

const rateLimit = require('./middleware/rate-limit');
app.use(rateLimit);

app.use(logger('combined'));
app.use(express.json());
app.use(express.urlencoded({extended: false}));
app.use(cookieParser());
app.use(express.static(path.join(__dirname, 'public')));

const timingMetrics = require("./middleware/statsd/timing")
app.use(timingMetrics)

const statusCodeMetrics = require("./middleware/statsd/status_code")
app.use(statusCodeMetrics)

const {prometheusMetrics, register} = require('./middleware/prometheus_metrics');
app.use(prometheusMetrics);

const sinkStats = require('./middleware/sink/stats');
const {toIpv4} = require('./middleware/sink/ip');
const excludedFromStats = ['/sink/stats', '/sink/recent', '/sink/config/latency', '/sink/config/failure', '/sink/config/response'];
const isSinkTraffic = path => path === '/sink' || path.startsWith('/sink/');
const isControlPlane = path => excludedFromStats.includes(path) || path === '/sink/projects' || path.startsWith('/sink/projects/');
const MAX_CAPTURED_RESPONSE_BYTES = 4096;
app.use((req, res, next) => {
    if (isSinkTraffic(req.path) && !isControlPlane(req.path)) {
        const method = req.method;
        const path = req.path;
        const remoteAddress = toIpv4(req.ip);
        const body = req.body;
        const userAgent = req.get('User-Agent');
        const headers = req.headers;
        const startTime = Date.now();

        const chunks = [];
        let capturedBytes = 0;
        let truncated = false;
        const capture = chunk => {
            if (!chunk || truncated) return;
            const buf = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
            const remaining = MAX_CAPTURED_RESPONSE_BYTES - capturedBytes;
            if (buf.length > remaining) {
                chunks.push(buf.subarray(0, remaining));
                capturedBytes = MAX_CAPTURED_RESPONSE_BYTES;
                truncated = true;
            } else {
                chunks.push(buf);
                capturedBytes += buf.length;
            }
        };
        const originalWrite = res.write.bind(res);
        const originalEnd = res.end.bind(res);
        res.write = function (chunk, encoding, callback) {
            capture(chunk);
            return originalWrite(chunk, encoding, callback);
        };
        res.end = function (chunk, encoding, callback) {
            if (chunk) capture(chunk);
            return originalEnd(chunk, encoding, callback);
        };

        res.on('finish', () => {
            const responseTimeMs = Date.now() - startTime;
            const responseHeaders = res.getHeaders();
            const contentType = String(responseHeaders['content-type'] || '');
            const isTextual = contentType === '' || /json|text|xml/.test(contentType);
            let responseBody;
            if (chunks.length > 0) {
                if (isTextual) {
                    let text = Buffer.concat(chunks).toString('utf8');
                    if (contentType.includes('json') && !truncated) {
                        try {
                            text = JSON.parse(text);
                        } catch (e) {
                            // leave as raw string
                        }
                    } else if (truncated) {
                        text += '... (truncated)';
                    }
                    responseBody = text;
                } else {
                    responseBody = `<binary response, ${capturedBytes}${truncated ? '+' : ''} bytes>`;
                }
            }
            sinkStats.record(method, path, remoteAddress, body, userAgent, headers, responseTimeMs, res.statusCode, responseHeaders, responseBody);
        });
    }
    next();
});

app.use('/', indexRouter);
app.use('/build-info', buildInfoRouter);
app.use('/probe/live', liveProbeRouter);
app.use('/probe/ready', readyProbeRouter);
app.get('/metrics', async (req, res) => {
    res.set('Content-Type', register.contentType);
    res.end(await register.metrics());
});
app.use('/sink', sinkRouter);


app.use((req, res, next) => {
    next(createError(404));
});

app.use((err, req, res, next) => {
    console.log("env = " + req.app.get('env'))
    res.locals.message = err.message;
    res.locals.error = req.app.get('env') === 'development' ? err : {};
    res.status(err.status || 500);
    res.render('error');
});


const http = require('http');
const https = require('https');
const httpServer = http.createServer(app);
const httpsServer = https.createServer(credentials, app);

const printServerIsUp = server => {
    const addr = server.address();
    const bind = typeof addr === 'string' ? 'pipe ' + addr : 'port ' + addr.port;
    console.log(`App ready on ${bind}`);
};

const PORT = process.env.PORT || 38080;
httpServer.listen(PORT);
httpServer.on('listening', () => {
    printServerIsUp(httpServer)
});

httpsServer.listen(8443);
httpServer.on('listening', () => {
    printServerIsUp(httpsServer)
});


module.exports = app;
