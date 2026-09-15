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

* **Debug what a client actually sends** — headers, body, query args, live, in Packet Inspection
* **Test client resilience** — inject latency/failures (or use a chaos preset) without touching real infra
* **Lightweight mocking** — stand in for a not-yet-built or unavailable endpoint with a response template
* **Load-testing target** — a safe place to point Gatling/load-gen scripts at and watch traffic live
* **Multi-user safety** — project-token namespacing so shared-instance configs don't collide
* **Zero-setup webhook/callback receiver** — confirm something fired and see the payload, no auth or setup

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

Every endpoint is instrumented with timing and status-code counters, available via StatsD and at `GET /metrics` (Prometheus format).

`route` is bucketed to the top-level path segment (e.g. everything under `/sink/*` becomes `route="/sink"`) since `/sink` accepts arbitrary echoed subpaths and unbounded label cardinality would grow the metrics endpoint unbounded.

## Contributing
Please send an issue or pull request. Thanks.


