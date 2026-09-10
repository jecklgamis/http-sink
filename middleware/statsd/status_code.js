const StatsdClient = require('statsd-client');
const statsd = new StatsdClient({host: 'localhost', port: 8125});
const onFinished = require('on-finished')


function statusCodeMetrics(req, res, next) {
    try {
        const url = req.originalUrl.replaceAll("/", ".")
        onFinished(res, function () {
            const statusCode = res.statusCode
            const errorCounter = `${url}.${classify(statusCode)}`
            statsd.increment(errorCounter, 1)

        })
    } finally {
        next()
    }
}

function classify(statusCode) {
    if (statusCode >= 200 && statusCode < 300) {
        return '2xx'
    }
    if (statusCode >= 300 && statusCode < 400) {
        return '3xx'
    }
    if (statusCode >= 400 && statusCode < 500) {
        return '4xx'
    }
    return '5xx'
}

module.exports = statusCodeMetrics