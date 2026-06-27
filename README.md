# xenon

**Streaming-native, open-source network monitoring** — built on gNMI/OpenConfig
telemetry and the Prometheus ecosystem. A modern, LibreNMS-style platform whose
engine is small and vendor-agnostic, and whose device support, dashboards, and
alert rules are **content**.

> Prototype. The control plane, collector integration, dashboards, multi-device
> graphing, and an app-native alarm engine work end to end — see the caveats at
> the bottom and the design docs in [`docs/`](docs/).

## Architecture

```
 gNMI/OpenConfig          xenond drives targets ▼          xenond reads ▲
 devices ──▶ gnmic collector ──scrape──▶ Prometheus ──PromQL──▶ xenond (GUI + control plane)
```

- **xenond** — one Go binary: the control/read plane and web GUI. It generates the
  collector's target list from its inventory (`GET /gnmic/targets`) and queries
  Prometheus for dashboards and alarm evaluation. It stays off the metrics hot path
  (collection keeps running if the app is down).
- **gnmic** — the OpenConfig gNMI collector; pulls its targets from xenond and
  exposes metrics for Prometheus to scrape.
- **Prometheus** — the metric store.

**Engine vs content:** the Go engine is vendor-agnostic; device profiles, detection
rules, and alert packs are JSON content under `internal/content/bundled/`.

## Features

- Operator-first GUI: fleet **Overview**, **Devices** with per-device tabs
  (Overview / Ports / Health / Alarms / Config), live inline-SVG graphs with
  click-to-drill-down (1h–7d), zero client-side charting libraries.
- **Explore** — multi-device, group-by-label graphing
  (`sum/avg/max by (site|role|platform|device)`), the cross-device view that
  RRD-per-device tools struggle with.
- **Alarms** — app-native engine: PromQL rules as content, evaluated on a loop,
  with lifecycle (active / cleared / ack).
- Live gNMI **onboarding**: point at a target → auto-detect platform → assign a profile.

## Run it

Engine CLI (zero dependencies, no device needed):

```sh
go run ./cmd/xenon        # signature → detection → profile → gNMIc config + cardinality estimate
```

Web app:

```sh
go run ./cmd/xenond                                    # GUI on :8080 (planning view)
XENON_PROM=http://localhost:9090 go run ./cmd/xenond   # + live graphs/alarms from Prometheus
```

For the full live experience, point `XENON_PROM` at a Prometheus that scrapes a
gnmic collector whose targets come from xenond's `GET /gnmic/targets`.

## Deploy

A container image and a Helm chart ([`charts/xenon`](charts/xenon)) are published to
a registry by [`.github/workflows/release.yml`](.github/workflows/release.yml) on
push/tag. The chart deploys xenond + gnmic + Prometheus into one namespace:

```sh
helm install xenon oci://ghcr.io/OWNER/charts/xenon \
  --namespace xenon --create-namespace \
  --set image.repository=ghcr.io/OWNER/xenond \
  --set ingress.host=xenon.example.com
```

Provide the gNMI credential as a Secret (`xenon-device-creds`, key `GNMIC_PASSWORD`);
see [`charts/xenon/values.yaml`](charts/xenon/values.yaml). The chart also works with
Flux via an `OCIRepository` + `HelmRelease`.

## Design

Design docs live in [`docs/`](docs/) — start with `goals.md`, `architecture.md`, and
`gui-architecture.md`.

## Status & caveats

Prototype. Current limitations: inventory and alarms are in-memory (no persistence);
operational status is derived from Prometheus liveness (the gNMIc session-state
signal isn't wired yet); the per-device metric labels are applied at the Prometheus
scrape for a single device (production attaches them per-target from the generated
gNMIc config); in-cluster onboarding needs a gNMI client bundled into the image.

## License

Not yet licensed — add a `LICENSE` before depending on this.
