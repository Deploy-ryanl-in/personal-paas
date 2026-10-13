# Reproducible subscription benchmark

Build the opt-in tool from the platform root with the version in go.mod:

```sh
go build -o paas-perf ./cmd/paas-perf
./paas-perf serve
# In another terminal:
./paas-perf -url http://127.0.0.1:8080 -connections 1000 -rps 100 -duration 120s -output result.json
```

The fixture acknowledges `{"op":"subscribe","topic":"ticks"}` and broadcasts
one sequenced 256-byte payload per second to every subscriber. Each connection
has a bounded four-event queue. HTTP `/api/payload` returns JSON with a 512-byte
payload and `Cache-Control: no-store`. `PERF_ROLE=peer` returns404 for both paths,
while retaining an independent `/healthz`; this tests shared route replication.

Run the same fixture under the same app limits for direct and platform paths.
A loopback gateway requires `-host <public-domain>`; TLS origin testing uses
`-url https://<public-domain> -dial 127.0.0.1:443`, preserving SNI and normal
certificate verification. Omitting `-dial` reaches normal DNS/Cloudflare. No
`insecure` mode is provided. Only test systems and domains you control.

The client ramps one connection every5ms (configurable with `-ramp`; use50ms for a gradual external ramp), waits for subscription acknowledgements,
then measures simultaneous event delivery and absolute-scheduled HTTP traffic.
All connections must remain active; every subscriber must receive at least
`durationSeconds-2` events. Errors, sequence gaps, failed acknowledgements,
HTTP non200/invalid payload, scheduler drops or achieved rate below99% of target
make the result fail with exit1. Cold/ramp time is reported separately. Short
smoke runs have weak event-count thresholds; use120seconds or longer for capacity
acceptance. Tests exercise real message validation and reject false success.

A zero-error benchmark proves this workload, not arbitrary DB queries, larger
messages, faster broadcasts, expensive handlers or unlimited connections.
Measure application CPU, memory, GC, descriptor count and network alongside
results. Run direct/gateway/TLS/edge separately; do not attribute edge latency to
the controller. Cleanup only disposable test containers/images and keep data.

For a temporary PaaS application, copy this directory plus the platform's
`go.mod`/`go.sum` into an independent app repository using the existing reusable
release workflow. Build context is`.` and Dockerfile path is
`cmd/paas-perf/Dockerfile`; declare port8080, `/healthz` status200,96MiB,0.5CPU,
128PIDs and a disposable owned domain. No secrets/volumes are required. The
application Docker image is built by CI, not by the VPS. Do not replace the
platform controller repository's CI with an app deployment workflow.
