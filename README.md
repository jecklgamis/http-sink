# http-sink

[![Build](https://github.com/jecklgamis/http-sink/actions/workflows/build.yaml/badge.svg)](https://github.com/jecklgamis/http-sink/actions/workflows/build.yaml)

A request sink for testing HTTP clients: `/sink` accepts any method or subpath, echoes back what it received, and lets you simulate latency and failures per path while watching live traffic stats on a built-in dashboard.

Docker:  `docker run --name http-sink -p 38080:38080 -it  jecklgamis/http-sink:main`

What's In The Box?

* `GET|POST|PUT|...  /sink` and `/sink/*` — echoes method, path, headers, body, query args, and origin IP as JSON (httpbin-style)
* Live dashboard at `/` — RPS sparkline, RPS by path & method, last-10-requests inspector (freeze/clear, click-to-expand headers/body), all polling `/sink/stats`
* Per-path **latency simulation** (`/sink/config/latency`) — add a random 0..N ms delay before responding
* Per-path **failure injection** (`/sink/config/failure`) — return a configured status code for a configured fraction of requests
* [Go](https://go.dev) app, Alpine [Docker](https://docker.io) image, HTTP and HTTPS listeners (self-signed certs)
* /build-info endpoint (returns Git branch, version, and build time info)
* /probe/ready, /probe/live endpoints for Kubernetes deployment
* Statsd metrics (response time, 2xx/3xx/4xx/5xx metrics)
* [Kubernetes](https://kubernetes.io/) Helm chart 

Have fun and hope you find this useful!

## Practical Uses

* **Debugging what a client actually sends** — point any HTTP client (webhook sender, API integration, mobile app, curl script) at `/sink/...` and see the exact method, headers, body, and query args it produced, live, in the Packet Inspection panel — no need to stand up a real backend or add logging to debug "why isn't my webhook payload what I expect."
* **Testing client resilience without touching real infrastructure** — the [latency/failure injection](#simulating-latency-and-failures) lets you simulate a slow or flaky downstream service (500s, timeouts, jitter) to verify your client's retry/backoff/timeout logic actually works, without needing chaos-engineering tooling like Toxiproxy or modifying a real service.
* **Load-testing target** — since it's a lightweight sink that just echoes and records stats, it's a safe place to point Gatling/load-gen scripts at and watch RPS/traffic patterns live instead of guessing from logs.
* **Sharing with a team** — latency/failure config is keyed by exact path, so people or test scenarios can share one instance by picking distinct paths (e.g. `/sink/team-a/checkout`). There is no auth or per-user isolation: request payloads on the dashboard are visible to everyone. Latency/failure configs expire after 1 hour (re-adding refreshes them), and at most 200 of each can exist at once. For real isolation, run one deployment (Helm release) per team.
* **Zero-setup webhook/callback receiver** — anything that needs to POST somewhere and you just want to confirm it fired and see the payload (third-party webhooks, CI notifications, IoT devices) can point at it with no auth, no schema, no setup.

## Simulating latency and failures

There are two ways to make the sink slow or flaky. Both apply to `/sink/...` requests only, and injected delay counts toward the reported response times.

### Per request (stateless)

Add parameters to the request itself, as query parameters or headers (the query parameter wins if both are given). Only that one request is affected, so people sharing an instance can't interfere with each other.

| Query parameter | Header | Meaning |
|---|---|---|
| `sinkDelay` | `X-Sink-Delay` | Delay before responding: `250ms`, `1.5s`, a bare number of milliseconds (`250`), or a random range (`50ms-300ms`). Maximum `10s`. |
| `sinkStatus` | `X-Sink-Status` | Respond with this status (400-599) instead of 200. Fails every time unless `sinkFailRate` is set. |
| `sinkFailRate` | `X-Sink-Fail-Rate` | Fraction of requests (0-1) that fail. Without `sinkStatus` the failure is a `500`. |

```
curl 'localhost:38080/sink/orders?sinkDelay=50ms-300ms'                  # random 50-300ms delay
curl 'localhost:38080/sink/orders?sinkStatus=503'                        # always 503
curl 'localhost:38080/sink/orders?sinkStatus=502&sinkFailRate=0.3'       # 30% of requests get a 502
curl -H 'X-Sink-Delay: 2s' -H 'X-Sink-Status: 504' localhost:38080/sink/orders
```

* The names are prefixed on purpose, so ordinary parameters your clients already send (`?status=active`, `?delay=5`) are just echoed back and never interpreted.
* Invalid values return `400` with an explanation, rather than being ignored.
* A request's own settings replace any path config for that request. A delayed response carries an `X-Sink-Injected-Delay-Ms` header with the delay that was applied.
* At most 500 requests can be sleeping on a delay at once. Beyond that the sink answers `429` immediately instead of queueing more.

### Per path (shared config)

For clients you can't modify to add parameters, configure an exact path on the dashboard or the API:

```
curl -X POST localhost:38080/sink/config/latency -H 'Content-Type: application/json' \
     -d '{"path": "/sink/team-a/checkout", "jitterMs": 200}'
curl -X POST localhost:38080/sink/config/failure -H 'Content-Type: application/json' \
     -d '{"path": "/sink/team-a/checkout", "rate": 0.25, "statusCode": 503}'
curl -X DELETE 'localhost:38080/sink/config/latency?path=/sink/team-a/checkout'
```

Path config is shared by everyone using the instance. Each config expires after 1 hour (setting it again refreshes it), and at most 200 of each type can exist (the API returns `429` beyond that).

## Requirements

* [Go](https://go.dev/doc/install) 1.26+
* [Docker](https://docs.docker.com/get-docker/)
* GNU Make

Most build and run commands are wrapped inside Makefile. Explore this one to see what the existing targets you can
invoke of if you like to add one.

## Building
```
make all
```

## Running
```
make up
```

Run app directly (no Docker) in development mode. This uses `go run -race` and shows error details on error pages.

```
make run-app-dev-mode
```

## Testing

Run unit tests (`go vet` plus all tests under `internal/`):

```
make tests
```

Run integration tests (all tests under `test/integration`, which exercise the full HTTP stack in-process):

```
make int-tests
```


All the endpoints are instrumented with timing and status code counters using Statsd.

* <endpoint.name>.duration - timer
* <endpoint.name>.hits - counter
* <endpoint.name>.2xx - counter
* <endpoint.name>.3xx - counter
* <endpoint.name>.4xx - counter
* <endpoint.name>.5xx - counter

## Contributing
Please send an issue or pull request. Thanks.

## Response time percentiles

Response times (including any injected latency) are tracked with [HdrHistogram](https://github.com/HdrHistogram/hdrhistogram-go):

* The dashboard shows p50/p75/p90/p95/p99/max over the last 30s, for all paths and the busiest ones. The same data is in `GET /sink/stats` under `latency`.
* `GET /metrics` serves Prometheus metrics for sink traffic: `http_sink_requests_total{method,code}` and the `http_sink_request_duration_seconds` histogram, since startup.
