const WINDOW_MS = 1000;
const DEFAULT_LIMIT = 1000;
const LIMIT = Number(process.env.RATE_LIMIT_RPS) > 0 ? Number(process.env.RATE_LIMIT_RPS) : DEFAULT_LIMIT;

let windowStart = Date.now();
let count = 0;

function rateLimit(req, res, next) {
    const now = Date.now();
    if (now - windowStart >= WINDOW_MS) {
        windowStart = now;
        count = 0;
    }
    count++;
    if (count > LIMIT) {
        return res.status(429).json({error: `rate limit exceeded: max ${LIMIT} requests per second`});
    }
    next();
}

function reset() {
    windowStart = Date.now();
    count = 0;
}

module.exports = rateLimit;
module.exports.reset = reset;
