# http-sink

[![Build](https://github.com/jecklgamis/http-sink/actions/workflows/build.yaml/badge.svg)](https://github.com/jecklgamis/http-sink/actions/workflows/build.yaml)

Relax, sink it. **http-sink** accepts any HTTP request, echoes back what it received, and shows your traffic live on a
dashboard. Make requests slow or flaky with a query parameter to see how your client copes.

## Try it

```
docker run -p 38080:38080 jecklgamis/http-sink:main
```

(or from source with Go 1.26+: `make run-app-dev-mode`)

Open http://localhost:38080, then send it something:

```
curl -X POST 'localhost:38080/sink/hello?x=1' -H 'Content-Type: application/json' -d '{"name": "world"}'
```

You get your request echoed back (method, path, query args, headers, body, origin) and it appears in the dashboard's
**Packet Inspection** table. Anything under `/sink` works: any method, any subpath.

Now make it misbehave:

```
curl 'localhost:38080/sink/hello?sinkDelay=2s'            # wait 2 seconds
curl 'localhost:38080/sink/hello?sinkStatus=503'          # fail with a 503
curl 'localhost:38080/sink/hello?sinkStatus=503:30'       # fail 30% of the time
```

## What it's good for

* **Seeing what a client really sends** — webhooks, API integrations, mobile apps, curl scripts.
* **Testing retries, backoff and timeouts** — simulate a slow or flaky service without touching a real one.
* **A load-test target** — watch requests per second and response-time percentiles live.
* **A throwaway webhook receiver** — no auth, no schema, no setup.

## Simulating latency and failures

Add these to any `/sink/...` request, as query parameters or headers (the query parameter wins). They affect only that
request, so nothing is stored and people sharing an instance can't interfere with each other.

| Query parameter | Header | Examples |
|---|---|---|
| `sinkDelay` | `X-Sink-Delay` | `250ms`, `1.5s`, `250` (milliseconds), `50ms-300ms` (random in a range) |
| `sinkStatus` | `X-Sink-Status` | `503`, any status from 400 to 599 |

**Probability lists** apply a value to only some requests, as `value:percent,value:percent`. The percentages must add up
to 100 or less, and the rest of the requests are left alone:

```
sinkStatus=503:10,429:5          10% get a 503, 5% get a 429, the rest succeed
sinkDelay=50ms:90,2s:10          90% wait 50ms, 10% wait 2 seconds
```

Good to know:

* The names are prefixed, so ordinary parameters your clients already send (`?status=active`) are just echoed back.
* Bad values get a `400` with an explanation, and delays are capped at `MAX_DURATION` (10s by default).
* Delayed responses carry an `X-Sink-Injected-Delay-Ms` header. Injected delay counts in the reported response times.

### For clients you can't change

If a client can't add parameters, preload rules for exact paths from a file and set `SINK_RULES_FILE` to its location
([`rules.example.json`](rules.example.json)):

```json
{
  "rules": [
    {"path": "/sink/orders",  "delay": "50ms-300ms", "status": "503:10,429:5"},
    {"path": "/sink/webhook", "status": "500:25"}
  ]
}
```

Same syntax as above. Rules match their exact path, are read once at startup, and can't be changed at runtime (the
dashboard lists them). A request's own parameters override a rule.

## Endpoints

| Endpoint | What it does |
|---|---|
| `ANY /sink`, `/sink/*` | Echo the request back (JSON) |
| `GET /` | The live dashboard |
| `GET /sink/stats` | The dashboard's data: RPS, recent requests, p50/p75/p90/p95/p99/max response times over 30s |
| `DELETE /sink/recent` | Clear the recent-requests table |
| `GET /sink/rules` | The configured rules |
| `GET /metrics` | Prometheus metrics for sink traffic |
| `GET /build-info` | Git branch, version and build time |
| `GET /probe/live`, `/probe/ready` | Kubernetes health probes |

HTTPS is served on `8443` as well when `server.crt` and `server.key` are present (`./generate-ssl-certs.sh` makes
self-signed ones). All endpoints also emit StatsD timers and counters (`<path>.duration`, `.hits`, `.2xx`..`.5xx`) to
`localhost:8125`.

## Configuration

Environment variables only; everything is optional.

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `38080` | HTTP port |
| `HTTPS_PORT` | `8443` | HTTPS port |
| `SINK_RULES_FILE` | none | Static per-path rules file |
| `MAX_DURATION` | `10s` | Longest delay a request or rule may ask for |
| `MAX_BODY_SIZE` | `102400` | Largest JSON or form body parsed and echoed, in bytes (larger gets a `413`) |
| `RATE_LIMIT` | `1000` | Requests per second per instance; `0` disables |
| `LOG_FORMAT` | `text` | Access log as `text` (Apache combined) or `json` |
| `APP_ENV` | none | `development` shows error details on error pages |

## Running more than one instance

http-sink keeps no database, volume or shared state, so you can run as many replicas as you like. Instances don't
coordinate, which has two consequences:

* **The dashboard shows one instance's traffic.** Behind a load balancer each refresh can land on a different replica,
  so the numbers jump around. Use sticky sessions at your ingress (see the commented annotation in the Helm chart's
  `values.yaml`) so a browser stays on one replica.
* **The rate limit is per instance**, so the effective limit is `RATE_LIMIT` times the number of replicas.

For one view across all replicas, scrape `/metrics` with Prometheus, which sums them, for example
`sum(rate(http_sink_requests_total[1m]))`.

The Helm chart (`deployment/k8s/helm`) can mount your rules from a ConfigMap (`rules` value) and pass extra
environment variables (`extraEnv`).

## Development

Needs Go 1.26+, Docker and GNU Make.

```
make run-app-dev-mode    # run locally with the race detector and error details
make tests               # go vet and unit tests
make int-tests           # integration tests (full HTTP stack, in-process)
make image               # build the Docker image
make all                 # tests, image and Helm chart
```

Run `make` to list the other targets.

## Contributing

Please send an issue or pull request. Thanks.
