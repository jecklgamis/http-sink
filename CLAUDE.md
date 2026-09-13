# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Install dependencies
make install-deps          # npm install + global mocha, nodemon

# Development
make run-app-dev-mode      # Run with nodemon auto-reload and debug enabled

# Testing
make tests                 # Unit tests: mocha --exit test/unit
make int-tests             # Integration tests: mocha --exit test/integration (requires SSL certs)
mocha --exit test/unit/some_unit_test.js     # Run a single test file

# Build
make dist                  # Generate SSL certs + build info
make image                 # Build Docker image
make run                   # Run Docker container (ports 38080/8443)
make all                   # Full build: tests + image + Helm chart
```

Integration tests require SSL certificates — run `./generate-ssl-certs.sh` if they're missing.

## Architecture

**Entry point**: `app.js` — creates an Express app, mounts middleware and routes, starts HTTP (38080 by default, configurable via PORT env var) and HTTPS (8443) servers.

**Routes** (`routes/`): Each file exports an Express router for one endpoint group:
- `index.js` → `GET /` — renders the live dashboard (`views/index.pug`)
- `blackhole.js` → `ALL /sink` and `ALL /sink/*` — echoes back method/path/headers/body/etc. (httpbin-style), applying any configured latency/failure simulation for the exact path first. Also serves:
  - `GET /sink/stats` — current traffic stats (polled by the dashboard every second)
  - `DELETE /sink/recent` — clears the "Packet Inspection" (last 10 requests) buffer
  - `GET/POST/DELETE /sink/config/latency` — manage per-path jitter delay config
  - `GET/POST/DELETE /sink/config/failure` — manage per-path failure-rate/status-code injection config
- `build_info.js` → `GET /build-info` (returns git branch, commit, build time from `build-info-data.js`)
- `live_probe.js` / `ready_probe.js` → `GET /probe/live` and `GET /probe/ready` (Kubernetes health probes)

**Middleware** (`middleware/`):
- `statsd/timing.js` — records request duration per endpoint (StatsD)
- `statsd/status_code.js` — counts responses by status class (2xx/3xx/4xx/5xx) per endpoint (StatsD)
- `blackhole/stats.js` — in-memory traffic recorder: tracks every non-config request (method, path, headers, body, user agent, remote address, response time, status code), computes 1s-window RPS/counts by path & method, keeps a 30-bucket RPS history for the dashboard sparkline, and a capped 10-entry "recent requests" ring buffer
- `blackhole/latency.js` — in-memory `path → jitterMs` map; `delayFor(path)` returns a random `0..jitterMs` delay
- `blackhole/failure.js` — in-memory `path → {rate, statusCode}` map; `statusCodeFor(path)` rolls the dice per request

**Front end**: `views/index.pug` + `public/javascripts/blackhole-stats.js` + `public/stylesheets/style.css` — a single-page dashboard that polls `/sink/stats` every second and renders: an RPS sparkline (30s history with grid/y-axis), RPS by path & method, the last-10-requests table (with freeze/clear controls and click-to-expand JSON cells), and the latency/failure config forms.

**Build metadata**: `build-info-data.js` is a generated file (via `generate-build-info.sh`) that contains `branch`, `version` (commit hash), and `build_time`. It is committed when building locally but regenerated in CI.

**Test framework**: Mocha + Chai + chai-http. Unit tests in `test/unit/`, integration tests in `test/integration/`.

**Deployment**: `deployment/k8s/helm/` contains a Helm chart for Kubernetes. The app exposes liveness/readiness probe endpoints for K8s health checks.

**CI/CD**: `.github/workflows/build.yml` — on push to `main` or PRs, it generates SSL certs and build info, then builds and pushes a Docker image to `jecklgamis/http-sink` (push only on `main`, not PRs).
