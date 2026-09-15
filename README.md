# http-sink

[![Build](https://github.com/jecklgamis/http-sink/actions/workflows/build.yaml/badge.svg)](https://github.com/jecklgamis/http-sink/actions/workflows/build.yaml)

A request sink for testing HTTP clients: `/sink` accepts any method or subpath, echoes back what it received, lets you simulate latency and failures (or mock a response entirely) per path, and watch live traffic stats on a built-in dashboard.

Docker:  `docker run --name http-sink -p 38080:38080 -it  jecklgamis/http-sink:main`

Live instance: [http-sink.jecklgamis.com](https://http-sink.jecklgamis.com)

## What's In The Box?

**Core**
* `/sink` and `/sink/*` — echoes method, path, headers, body, query args, and origin IP (httpbin-style)
* httpbin-style utilities — `/sink/headers`, `/ip`, `/user-agent`, `/uuid`, `/status/:code`, `/delay/:seconds`, `/redirect/:n`, `/response-headers`, `/stream/:n`, `/bytes/:n`
* Live dashboard at `/` — traffic stats, endpoint reference with one-click testing, request/response inspector, all config forms

**Chaos & mocking**
* Per-path latency simulation and failure injection, plus one-click chaos presets (flaky network, slow DB, intermittent 5xx)
* Per-path (optionally per-method) response templates for lightweight mocking
* Config persists to disk, survives a restart

**Ops**
* `/build-info`, `/probe/ready`, `/probe/live`
* StatsD metrics + a Prometheus `/metrics` endpoint
* HTTP + HTTPS listeners, Alpine Docker image, Kubernetes Helm chart

See `CLAUDE.md` for full endpoint/config details.

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

All build/run/test commands are Makefile targets — see the [Makefile](Makefile) for the full list.

```bash
make install-deps      # npm install + global mocha, nodemon
make up                # tests + build + run (Docker)
make run-app-dev-mode   # run locally with nodemon + verbose logging
make tests              # unit tests (test/unit)
make int-tests          # integration tests (test/integration)
```

## Metrics

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


