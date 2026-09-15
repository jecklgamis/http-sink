const WINDOWS = [
    {label: '1s', seconds: 1},
];

const RPS_HISTORY_SECONDS = 30;
const MAX_AGE_MS = Math.max(WINDOWS[WINDOWS.length - 1].seconds, RPS_HISTORY_SECONDS) * 1000;
const RECENT_LIMIT = 10;

let requests = [];
let recent = [];

function record(method, path, remoteAddress, body, userAgent, headers, responseTimeMs, statusCode, responseHeaders, responseBody) {
    const hasBody = body && (typeof body !== 'object' || Object.keys(body).length > 0);
    const entry = {
        timestamp: Date.now(),
        method,
        path,
        remoteAddress,
        userAgent,
        headers,
        responseTimeMs,
        statusCode,
        body: hasBody ? body : undefined,
        responseHeaders,
        responseBody,
    };
    requests.push(entry);
    recent.push(entry);
    if (recent.length > RECENT_LIMIT) recent.shift();
}

function prune(now) {
    const cutoff = now - MAX_AGE_MS;
    let i = 0;
    while (i < requests.length && requests[i].timestamp < cutoff) i++;
    if (i > 0) requests = requests.slice(i);
}

function computeStats() {
    const now = Date.now();
    prune(now);

    const windows = {};
    for (const {label, seconds} of WINDOWS) {
        const cutoff = now - seconds * 1000;
        const byMethod = {};
        const byPath = {};
        const byPathAndMethod = {};
        let total = 0;
        for (const r of requests) {
            if (r.timestamp < cutoff) continue;
            byMethod[r.method] = (byMethod[r.method] || 0) + 1;
            byPath[r.path] = (byPath[r.path] || 0) + 1;
            byPathAndMethod[r.path] = byPathAndMethod[r.path] || {};
            byPathAndMethod[r.path][r.method] = (byPathAndMethod[r.path][r.method] || 0) + 1;
            total++;
        }
        const rpsByPathAndMethod = {};
        for (const p of Object.keys(byPathAndMethod)) {
            rpsByPathAndMethod[p] = {};
            for (const m of Object.keys(byPathAndMethod[p])) {
                rpsByPathAndMethod[p][m] = Number((byPathAndMethod[p][m] / seconds).toFixed(3));
            }
        }

        windows[label] = {
            seconds,
            total,
            byMethod,
            byPath,
            byPathAndMethod,
            rpsByPathAndMethod,
            rps: Number((total / seconds).toFixed(3)),
        };
    }

    const recentRequests = recent.slice().reverse();

    const rpsHistory = [];
    for (let i = RPS_HISTORY_SECONDS - 1; i >= 0; i--) {
        const bucketEnd = now - i * 1000;
        const bucketStart = bucketEnd - 1000;
        let count = 0;
        for (const r of requests) {
            if (r.timestamp >= bucketStart && r.timestamp < bucketEnd) count++;
        }
        rpsHistory.push(count);
    }

    return {now, windows, recentRequests, rpsHistory};
}

function clearRecent() {
    recent = [];
}

module.exports = {record, computeStats, clearRecent};
