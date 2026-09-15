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
- `sink.js` → `ALL /sink` and `ALL /sink/*` — echoes back method/path/headers/body/etc. (httpbin-style), applying any configured latency/failure simulation for the exact path first. Also serves:
  - `GET /sink/stats` — current traffic stats (polled by the dashboard every second)
  - `DELETE /sink/recent` — clears the "Packet Inspection" (last 10 requests) buffer
  - `GET/POST/DELETE /sink/config/latency` — manage per-path jitter delay config
  - `GET/POST/DELETE /sink/config/failure` — manage per-path failure-rate/status-code injection config
  - `GET/POST/DELETE /sink/config/response` — manage per-path (optionally per-method) response templates (fixed status code/headers/JSON body) for lightweight mocking; checked after failure injection, so a failure config on the same path still takes precedence. A template with no method matches any method; a method-specific template (GET/POST/PUT/DELETE/PATCH/HEAD/OPTIONS) takes precedence over one with no method on the same path. `DELETE` takes an optional `method` query param to target a specific method-scoped template
  - httpbin-style utility endpoints: `GET /sink/headers`, `/sink/ip`, `/sink/user-agent`, `/sink/uuid`, `/sink/status/:code`, `/sink/delay/:seconds` (capped at 10s), `/sink/redirect/:n` (capped at 20 hops), `/sink/response-headers` (echoes query params as response headers), `/sink/stream/:n` (capped at 100 lines), `/sink/bytes/:n` (capped at 100KB)
- `build_info.js` → `GET /build-info` (returns git branch, commit, build time from `build-info-data.js`)
- `live_probe.js` / `ready_probe.js` → `GET /probe/live` and `GET /probe/ready` (Kubernetes health probes)

`GET /metrics` (registered directly in `app.js`, not its own router file) exposes Prometheus-format metrics via `middleware/prometheus_metrics.js` — request count/duration histograms labeled by method, route, and status code, plus Node.js default process metrics. This is separate from and in addition to the existing StatsD emission (`middleware/statsd/*`); it doesn't replace it. The `route` label buckets by top-level path segment only (e.g. everything under `/sink/*` becomes `route="/sink"`) to keep cardinality bounded, since `/sink` accepts arbitrary echoed subpaths.

**Middleware** (`middleware/`):
- `statsd/timing.js` — records request duration per endpoint (StatsD)
- `statsd/status_code.js` — counts responses by status class (2xx/3xx/4xx/5xx) per endpoint (StatsD)
- `prometheus_metrics.js` — `@prometheus-io/client`-based request duration histogram and request counter, labeled by method/route/status code (route bucketed to top-level path segment), plus Node.js default process metrics; backs `GET /metrics`
- `sink/stats.js` — in-memory traffic recorder: tracks every non-config request (method, path, headers, body, response headers/body (captured via `res.write`/`res.end` interception in `app.js`, capped at 4KB, binary responses summarized), user agent, remote address, response time, status code), computes 1s-window RPS/counts by path & method, keeps a 30-bucket RPS history for the dashboard sparkline, and a capped 10-entry "recent requests" ring buffer
- `sink/latency.js` — `path → jitterMs` map; `delayFor(path)` returns a random `0..jitterMs` delay
- `sink/failure.js` — `path → {rate, statusCode}` map; `statusCodeFor(path)` rolls the dice per request
- `sink/response_template.js` — `"METHOD:path" → {statusCode, headers, body}` map (method defaults to `ANY`); `templateFor(path, method)` checks the exact method first, then falls back to the `ANY`-method template, checked after failure injection so a failure config still takes precedence over a template on the same path
- `sink/projects.js`, `sink/latency.js`, `sink/failure.js`, `sink/response_template.js` persist through `sink/store.js` — a JSON file per store under `SINK_DATA_DIR` (default `./data`), loaded at startup and rewritten on every mutation. Single swap point if this ever needs a networked backend (Redis/etcd) instead — only `store.js` would change, not the call sites, though a networked backend would make `persist()`/load async. Not wired into the Helm chart yet (no PVC), so in k8s this only survives a pod restart, not rescheduling, and only works correctly with a single replica — this is a sink-only concern; `sink/stats.js` (traffic history) stays in-memory/ephemeral by design

**Front end**: `views/index.pug` + `public/javascripts/sink-stats.js` + `public/stylesheets/style.css` — a single-page dashboard that polls `/sink/stats` every second and renders: an RPS sparkline (30s history with grid/y-axis), RPS by path & method, the last-10-requests table (with freeze/clear controls and click-to-expand JSON cells), the latency/failure/response-template config forms, and a "Chaos Presets" section (Flaky network / Slow DB / Intermittent 5xx) that's pure client-side sugar -- it just POSTs canned values to the existing `/sink/config/latency` and `/sink/config/failure` endpoints for a given path, no new backend route.

**Build metadata**: `build-info-data.js` is a generated file (via `generate-build-info.sh`) that contains `branch`, `version` (commit hash), and `build_time`. It is committed when building locally but regenerated in CI.

**Test framework**: Mocha + Chai + chai-http. Unit tests in `test/unit/`, integration tests in `test/integration/`.

**Deployment**: `deployment/k8s/helm/` contains a Helm chart for Kubernetes. The app exposes liveness/readiness probe endpoints for K8s health checks.

**CI/CD**: two workflows, both triggered on push to `main`, PRs, and `v*` tags (`build.yaml`) or `v*` tags only (`release.yaml`):
- `.github/workflows/build.yaml` — generates SSL certs and build info, runs tests, then builds and pushes a Docker image to `jecklgamis/http-sink` (push skipped for PR builds); tags are derived from the git ref via `docker/metadata-action`, so a `v1.2.3` tag produces matching image tags.
- `.github/workflows/release.yaml` — tag-push only. Validates the tag matches `vMAJOR.MINOR.PATCH[-prerelease]`, re-runs tests, packages the Helm chart (`deployment/k8s/helm/chart`) with that version, builds a source tarball with `package.json`'s version bumped to match, and creates a GitHub Release (auto-generated notes) with the Helm chart `.tgz` and source archive attached.

So `git tag v1.2.3 && git push --tags` triggers both: a tagged Docker image push and a GitHub Release with Helm chart + source archive. No manual npm publish step.
