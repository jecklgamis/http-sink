# http-sink

[![Build](https://github.com/jecklgamis/http-sink/actions/workflows/build.yaml/badge.svg)](https://github.com/jecklgamis/http-sink/actions/workflows/build.yaml)

A request sink for testing HTTP clients: `/sink` accepts any method or subpath, echoes back what it received, and lets you simulate latency and failures per path while watching live traffic stats on a built-in dashboard.

Docker:  `docker run --name http-sink -p 38080:38080 -it  jecklgamis/http-sink:main`

What's In The Box?

* `GET|POST|PUT|...  /sink` and `/sink/*` — echoes method, path, headers, body, query args, and origin IP as JSON (httpbin-style)
* httpbin-style utility endpoints: `/sink/headers`, `/sink/ip`, `/sink/user-agent`, `/sink/uuid`, `/sink/status/:code`, `/sink/delay/:seconds`, `/sink/redirect/:n`, `/sink/response-headers`, `/sink/stream/:n`, `/sink/bytes/:n`
* Live dashboard at `/` — RPS sparkline, RPS by path & method, last-10-requests inspector (freeze/clear, click-to-expand request **and response** headers/body), a reference table of every `/sink` endpoint with one-click "Test it!" buttons, and the config forms below, all polling `/sink/stats`
* Per-path **latency simulation** (`/sink/config/latency`) — add a random 0..N ms delay before responding
* Per-path **failure injection** (`/sink/config/failure`) — return a configured status code for a configured fraction of requests
* Per-path (optionally per-method) **response templates** (`/sink/config/response`) — return a fixed status code/headers/JSON body instead of the normal echo, for lightweight mocking
* **Chaos presets** — one-click "Flaky network" / "Slow DB" / "Intermittent 5xx" buttons that apply canned latency+failure combos to a path
* Project/config state (projects, latency, failure, response-template configs) **persists to disk** (JSON files under `SINK_DATA_DIR`, default `./data`) so it survives a process restart
* [ExpressJS](https://expressjs.com/) app, Alpine [Docker](https://docker.io) image, HTTP and HTTPS listeners (self-signed certs)
* /build-info endpoint (returns Git branch, version, and build time info)
* /probe/ready, /probe/live endpoints for Kubernetes deployment
* Metrics: Statsd (response time, 2xx/3xx/4xx/5xx counters) and a Prometheus-format `/metrics` endpoint (request count/duration histograms, Node.js process metrics)
* [Kubernetes](https://kubernetes.io/) Helm chart 

Have fun and hope you find this useful!

## Practical Uses

* **Debugging what a client actually sends** — point any HTTP client (webhook sender, API integration, mobile app, curl script) at `/sink/...` and see the exact method, headers, body, and query args it produced, live, in the Packet Inspection panel — no need to stand up a real backend or add logging to debug "why isn't my webhook payload what I expect."
* **Testing client resilience without touching real infrastructure** — the latency/failure injection (or a one-click chaos preset) lets you simulate a slow or flaky downstream service (500s, timeouts, jitter) to verify your client's retry/backoff/timeout logic actually works, without needing chaos-engineering tooling like Toxiproxy or modifying a real service.
* **Lightweight mocking** — response templates let a path return a fixed status/headers/JSON body instead of the echo, so you can stand in for a not-yet-built or currently-unavailable endpoint without writing a real mock server.
* **Load-testing target** — since it's a lightweight sink that just echoes and records stats, it's a safe place to point Gatling/load-gen scripts at and watch RPS/traffic patterns live instead of guessing from logs.
* **Multi-user/multi-scenario safety** — project-token namespacing means multiple people or test scenarios can share one deployed instance without one person's chaos config breaking another's traffic.
* **Zero-setup webhook/callback receiver** — anything that needs to POST somewhere and you just want to confirm it fired and see the payload (third-party webhooks, CI notifications, IoT devices) can point at it with no auth, no schema, no setup.

## Requirements

* [NodeJs](https://nodejs.org/en/download/package-manager/)
* [Docker](https://docs.docker.com/get-docker/)
* GNU Make

Most build and run commands are wrapped inside Makefile. Explore this one to see what the existing targets you can
invoke of if you like to add one.

## Building
```
make install-deps
make all
```

## Running
```
make up
```

Run app in development mode. This uses `nodemon` to auto reload modified files and enables verbose logging.

```
make run-app-dev-mode
```

## Testing

Run unit tests (all tests under `test/unit`):

```
make tests
```

Run integration tests (all tests under `test/integration`):

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

The same request/response data is also exposed in Prometheus format at `GET /metrics`:

* `http_requests_total{method,route,status_code}` - counter
* `http_request_duration_seconds{method,route,status_code}` - histogram
* plus Node.js default process metrics (memory, event loop lag, GC, etc.)

`route` is bucketed to the top-level path segment (e.g. everything under `/sink/*` becomes `route="/sink"`) since `/sink` accepts arbitrary echoed subpaths and unbounded label cardinality would grow the metrics endpoint unbounded.

## Contributing
Please send an issue or pull request. Thanks.


