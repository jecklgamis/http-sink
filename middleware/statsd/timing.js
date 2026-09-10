const StatsdClient = require('statsd-client');
const statsd = new StatsdClient({host: "localhost", port: 8125});
const onHeaders = require('on-headers')
const onFinished = require('on-finished')

function recordStartTime() {
    this.__startTime = new Date()
}

function timingMetrics(req, res, next) {
    try {
        const url = req.originalUrl.replaceAll('/', '.');
        const duration = `${url}.duration`
        const hits = `${url}.hits`
        recordStartTime.call(req)
        onHeaders(res, function () {
            recordStartTime()
        })
        onFinished(res, function () {
            statsd.timing(duration, req.__startTime)
            statsd.increment(hits, 1)
        });
    } finally {
        next()
    }
}

module.exports = timingMetrics