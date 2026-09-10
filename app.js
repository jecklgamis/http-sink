const createError = require('http-errors');
const express = require('express');
const path = require('path');
const cookieParser = require('cookie-parser');
const logger = require('morgan');

const indexRouter = require('./routes/index');
const buildInfoRouter = require('./routes/build_info.js');
const blackholeRouter = require('./routes/blackhole.js');
const liveProbeRouter = require('./routes/live_probe.js');
const readyProbeRouter = require('./routes/ready_probe.js');

const fs = require('fs');
const privateKey = fs.readFileSync('server.key', 'utf8');
const certificate = fs.readFileSync('server.crt', 'utf8');
const credentials = {key: privateKey, cert: certificate};
const app = express();

app.set('views', path.join(__dirname, 'views'));
app.set('view engine', 'pug');

// behind nginx-ingress: trust X-Forwarded-For so req.ip is the real client, not the ingress pod
app.set('trust proxy', true);

app.use(logger('combined'));
app.use(express.json());
app.use(express.urlencoded({extended: false}));
app.use(cookieParser());
app.use(express.static(path.join(__dirname, 'public')));

const timingMetrics = require("./middleware/statsd/timing")
app.use(timingMetrics)

const statusCodeMetrics = require("./middleware/statsd/status_code")
app.use(statusCodeMetrics)

const blackholeStats = require('./middleware/blackhole/stats');
const toIpv4 = ip => ip === '::1' ? '127.0.0.1' : ip.replace(/^::ffff:/, '');
const excludedFromStats = ['/blackhole/stats', '/blackhole/recent', '/blackhole/config/latency', '/blackhole/config/failure'];
const isBlackholeTraffic = path => path === '/blackhole' || path.startsWith('/blackhole/');
app.use((req, res, next) => {
    if (isBlackholeTraffic(req.path) && !excludedFromStats.includes(req.path)) {
        const method = req.method;
        const path = req.path;
        const remoteAddress = toIpv4(req.ip);
        const body = req.body;
        const userAgent = req.get('User-Agent');
        const headers = req.headers;
        const startTime = Date.now();
        res.on('finish', () => {
            const responseTimeMs = Date.now() - startTime;
            blackholeStats.record(method, path, remoteAddress, body, userAgent, headers, responseTimeMs, res.statusCode);
        });
    }
    next();
});

app.use('/', indexRouter);
app.use('/build-info', buildInfoRouter);
app.use('/probe/live', liveProbeRouter);
app.use('/probe/ready', readyProbeRouter);
app.use('/blackhole', blackholeRouter);


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
