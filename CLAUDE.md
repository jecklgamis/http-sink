# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Development
make build                 # go build -o http-sink .
make run-app-dev-mode      # APP_ENV=development go run -race . (HTTP 38080 / HTTPS 8443)

# Testing
make tests                 # go vet + unit tests: go test -race ./internal/...
make int-tests             # Integration tests: go test -race ./test/integration/...
go test -race -run TestName ./internal/sink    # Run a single test

# Build
make dist                  # Generate SSL certs + build info
make image                 # Build Docker image (multi-stage, static Go binary on alpine)
make run                   # Run Docker container (ports 38080/8443)
make all                   # Full build: tests + image + Helm chart
```

HTTPS needs `server.crt`/`server.key` in the working directory (run `./generate-ssl-certs.sh`). If they are missing the app logs a warning and serves HTTP only. Tests do not need certs (they run the handler in-process via `httptest`).

## Architecture

Go, standard library plus one dependency: `github.com/HdrHistogram/hdrhistogram-go` (response-time percentiles).

**Entry point**: `main.go` — builds the `server.Server`, starts HTTP (38080 by default, configurable via `PORT`) and HTTPS (8443) listeners, and shuts down gracefully on SIGINT/SIGTERM. `APP_ENV=development` adds error details to error pages.

**`internal/server`** — the HTTP surface. `Server` implements `http.Handler` and is built with `server.New(Options)`; all state is per-instance (tests get isolation by creating a new one).
- `server.go` — `New`, the middleware chain order, and `route` (a manual dispatcher on the raw request path, not `http.ServeMux`, so odd paths like `//` or `/..` reach the sink unmodified). Serves `GET /` (dashboard), `/build-info`, `/probe/live`, `/probe/ready`, and `/metrics` (Prometheus text format).
- `sink.go` — everything under `/sink`:
  - `ANY /sink` and `ANY /sink/*` — echoes method/path/url/args/headers/body/origin (httpbin-style) after applying any configured latency, then failure injection, for the *exact* path
  - `GET /sink/stats` — traffic stats (polled by the dashboard every second)
  - `DELETE /sink/recent` — clears the "Packet Inspection" (last 10 requests) buffer
  - `GET/POST/DELETE /sink/projects[/name]` — project (namespace) registry; creating returns a token shown once
  - `GET/POST/DELETE /sink/config/latency` and `/sink/config/failure` — per-path config; POST/DELETE must be under `/sink/<project>/...` and carry that project's `X-Sink-Token`
  - the `recordSinkStats` middleware, which records every non-control-plane `/sink` request
- `middleware.go` — rate limit (global 1000 req/s → 429), combined-format access log, embedded static assets, StatsD metrics (`<url>.duration|hits|2xx..5xx`, `/` → `.`), plus the response `recorder`.
- `body.go` / `request.go` — JSON (objects/arrays only) and urlencoded body parsing (100kb limit, other content types are left unparsed), and request helpers (client IP honouring `X-Forwarded-For`, lower-cased header map, etc.).

Middleware order (outermost first): rate limit → access log → body parse → static assets → statsd → sink stats → route. Static assets are served before statsd/stats on purpose, so they are not counted.

**`internal/sink`** — in-memory state, each type guarded by a mutex:
- `Stats` — 1s-window RPS/counts by path & method, 30-bucket RPS history for the sparkline, and a capped 10-entry "recent requests" buffer. Lightweight per-request samples are kept separately from the full entries so headers/bodies are only retained for the recent buffer.
- `histogram.go` / `Stats.Compute` — response-time percentiles (p50/p90/p99/p99.9/max, in ms) over the trailing 30s, overall and for the 10 busiest paths, returned as `latency` in `/sink/stats`. HDR histograms (microsecond resolution) are built on demand from the retained samples, so per-path state never accumulates.
- `Metrics` — lifetime counters (`http_sink_requests_total{method,code}`) and an HDR histogram of response times, rendered by `/metrics` as `http_sink_request_duration_seconds` (fixed Prometheus buckets derived from the HDR distribution). Only `/sink` data traffic is observed, same as the dashboard stats.
- `Latency` — `path → jitterMs`; `DelayFor(path)` returns a random `0..jitterMs` delay.
- `Failure` — `path → {rate, statusCode}`; `StatusCodeFor(path)` rolls the dice per request.
- `Projects` — name → token (constant-time verify).

**Other packages**: `internal/ratelimit` (fixed-window limiter), `internal/statsd` (fire-and-forget UDP client to localhost:8125; nil client is a no-op), `internal/buildinfo`.

**Front end** (`web/`, embedded with `go:embed`): `web/templates/*.html` (`html/template`) + `web/public/javascripts/sink-stats.js` + `web/public/stylesheets/style.css` — a single-page dashboard that polls `/sink/stats` every second and renders an RPS sparkline, RPS by path & method, the last-10-requests table, and the project/latency/failure forms.

**Build metadata**: `internal/buildinfo/data.go` is generated by `generate-build-info.sh` (`branch`, `version` = commit hash, `build_time`). It is committed when building locally but regenerated in CI.

**Tests**: standard `testing` package. Unit tests live beside the code in `internal/*`; integration tests in `test/integration/` drive the full handler through `httptest`.

**Deployment**: `deployment/k8s/helm/` contains a Helm chart for Kubernetes. The app exposes liveness/readiness probe endpoints for K8s health checks.

**CI/CD**: `.github/workflows/build.yaml` — on push to `main` or PRs, it generates SSL certs and build info, runs `go vet` and `go test -race ./...`, then builds and pushes a Docker image to `jecklgamis/http-sink` (push only on `main`, not PRs).
