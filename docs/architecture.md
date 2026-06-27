# xenon — Top-Level Architecture

**Status:** draft · 2026-06-22
**Scope:** end-state *target* architecture (not the migration path).
**Project constraints:** Junos-only · gNMI/OpenConfig · metrics-only (no logs) · <5,000 devices ·
fully OSS · *simple to **operate*** (small team runs it) · building a custom application.

This document defines the **top-level shape and the interfaces between components**. The internal
breakdown of the application (C4) lives in **`docs/c4-application.md`**.

> IDs are stable handles for discussion: components `C#`, interfaces `I#`, invariants `INV-#`.

---

## 1. Mental model — three planes

- **Data plane (autonomous):** `devices → gNMIc → Prometheus`. Runs continuously, *independent of the app*.
- **Control plane (app-driven):** `app inventory → gNMIc target/subscription config`.
- **Observe plane (app-driven):** `app → Prometheus (PromQL) → dashboards + alarm engine → UI + notifications`.

The application participates in the **control** and **observe** planes only. It is **not** in the data plane.

---

## 2. Components

| ID | Component | Plane(s) | Responsibility | Boundary |
|----|-----------|----------|----------------|----------|
| **C1** | Network devices (Junos) | data | Expose gNMI/OpenConfig streaming telemetry (dial-in) | external |
| **C2** | gNMIc (collector) | data + control | Subscribe to devices; expose `/metrics`; execute app-supplied target/subscription config | internal |
| **C3** | Metrics store · *default impl: Prometheus* | data + observe | Ingest via I2 (scrape); store metrics; serve vanilla-PromQL via I4. A **role**, not a fixed product — see §7 | internal |
| **C4** | Application (Go modular monolith) | control + observe | Inventory/SoT, collection-control, metrics query, alarm engine, dashboards, API, auth/RBAC, UI | internal |
| **C5** | Relational store (SQLite or Postgres) | — | App's **private** backing store: inventory, alarms, users, config. NOT a shared seam | internal to C4 |
| **C6** | External SoT (NetBox / CSV / YAML) | control | *Optional* upstream inventory feed via pluggable adapter | external · optional |
| **C7** | Operators (humans + automation) | observe + control | Consume UI/API | external |
| **C8** | Notification targets (email/Slack/webhook/PagerDuty) | observe | Receive alarm notifications | external |

---

## 3. Interfaces (the contracts)

### I1 · Device → gNMIc — telemetry subscription
- **Direction:** gNMIc dials in to device and subscribes (pull-initiated, server-streamed).
- **Protocol/port:** gNMI over gRPC/TLS. Port typically `:9339` (OpenConfig standard) or `:32767`
  (Junos default; version-dependent — verify per platform/release).
- **Payload:** `SubscribeRequest` (paths + mode `sample`/`on_change` + interval) → stream of `Notification`s
  (path, value, device timestamp).
- **Auth:** username/password or client cert, mapped onto a Junos **login class** (authz can succeed while
  specific paths return permission-denied — verify the service-account class).
- **Contract:** OpenConfig paths first; native `/junos/...` only where OC doesn't model the data.

### I2 · C2 → C3 — metrics ingest
- **Mechanism — DECISION: scrape (pull), NOT remote_write.** C3 scrapes gNMIc's `/metrics`.
  - *Why scrape here:* with no message bus, gNMIc holds current values in its cache and exposes them — exactly
    the shape scrape wants. The reference stacks (gnp-stack, netops-stack) use remote_write only because a NATS
    bus sits in the middle and the terminal emitter drains the queue (a push source); remote_write is downstream
    of *their bus choice*, not an endorsement of push over pull. No bus ⇒ scrape is the natural fit.
  - *Optionality is NOT bought here.* C3-swap insulation lives at the **query seam (I4/PromQL)**, not at ingest.
    Both scrape and remote_write are standards; only push-*only* backends (Mimir/Cortex — excluded) force
    remote_write, and the real growth path (Prometheus → HA pair → Thanos) stays scrape throughout. gNMIc also
    supports a `prometheus_write` output, so flipping the mechanism later is a collector-config change — we keep
    the *capability*, we do not *run* it now (it would cost scrape's `up`/staleness/in-order simplicity for
    optionality toward backends we've ruled out). See INV-8 / §7.
- **Protocol/port:** HTTP `GET /metrics`, Prometheus exposition format; gNMIc prometheus output `:9804`.
- **Cadence:** Prometheus `scrape_interval` — **MUST be ≤ the gNMI `sample_interval`** or samples are dropped.
- **Contract (CRITICAL):** the **metric naming + label schema** — the shared contract binding C2, C3, C4.
  **Device identity MUST be a label** (stable key matching app inventory). Simultaneously the cardinality lever
  and the app↔metrics correlation key. Versioned, co-owned artifact.

### I3 · App → gNMIc — desired collection state
- **Direction:** app publishes desired target/subscription state; gNMIc consumes it.
- **Mechanism (sub-choice, contract is the same):** (a) gNMIc **HTTP loader** polls an app endpoint
  *(preferred — declarative pull, no shared FS)*; (b) app writes a **targets file** gNMIc watches
  *(simplest/local)*; (c) app calls gNMIc **REST API** `:7890` *(imperative push, for dynamic per-target ops)*.
- **Payload:** target list (address, credential reference, subscription-profile bindings) derived from C4 inventory.
- **Cadence:** reconcile on inventory change.
- **Contract:** the **app is the sole author** of gNMIc's target config; gNMIc is the execution engine.
  Credentials are supplied by reference (app holds/encrypts or points at a secret store — sensitive seam).

### I4 · App → Prometheus — metrics read
- **Direction:** app queries Prometheus (read-only).
- **Protocol/port:** Prometheus HTTP API (`/api/v1/query`, `/query_range`, `/series`, …) `:9090`; PromQL → JSON.
- **Cadence:** on-demand (dashboard render, API) + periodic (alarm-engine evaluation loop).
- **Contract:** **read-only — the app never writes metrics.** Queries scope by the I2 label schema.

### I5 · External SoT → App — optional inventory sync
- **Direction:** app pulls from external SoT (upstream → app).
- **Protocol:** NetBox REST/GraphQL, or CSV/YAML import. Pluggable `InventorySource` adapter.
- **Cadence:** scheduled / on-demand / webhook-triggered.
- **Contract:** external SoT is **upstream and non-authoritative for monitoring**; the app reconciles records
  into its own inventory, which stays authoritative for *what is monitored*. One-way by default. NetBox is
  never load-bearing (see INV-2).

### I6 · Operators ↔ App — UI / API
- **Direction:** bidirectional.
- **Protocol:** HTTPS. UI = server-rendered (HTMX/SSE) or SPA; API = REST/JSON (and/or gRPC).
- **Auth:** session / OIDC; RBAC. Live updates via SSE/WebSocket (alarm console, device state).
- **Contract:** the app's versioned external surface (inventory CRUD, dashboards, alarm ack/clear, config).

### I7 · App → Notification targets — alarm notifications
- **Direction:** outbound (egress) on alarm state transitions.
- **Protocol:** SMTP / Slack / generic webhook / PagerDuty, via pluggable notifiers.
- **Contract:** notifications are a function of the app's alarm engine (raise/clear lifecycle events).

### I8 · App ↔ Relational store — *(internal, not a public seam)*
- SQL over local file (SQLite) or wire (Postgres). Only C4 touches C5; listed for completeness.

---

## 4. Invariants

- **INV-1 — App is off the data hot path.** Collection (I1+I2) runs independent of C4. App down ⇒ UI, alarm
  evaluation, and control degrade, but **no metric loss and no collection stoppage**.
- **INV-2 — The app's inventory is the single source of monitoring truth.** A device is monitored iff app
  inventory says so. External SoT (C6) feeds it but is not authoritative for monitoring.
- **INV-3 — The label/metric schema (I2) is the binding contract** across C2/C3/C4. Device identity is a label.
- **INV-4 — Prometheus is read-only to the app (I4).** The app never writes metrics.
- **INV-5 — gNMIc config is derived, never hand-edited.** App holds desired state and reconciles (I3).
- **INV-6 — Read-only to devices (metrics-only scope).** The only device-facing path is gNMI subscription (I1).
  Any future config/action path to devices is a NEW seam, explicitly out of scope today.
- **INV-7 — Every external seam is a standard protocol** (gNMI, Prometheus exposition + HTTP API, REST), so each
  component is independently replaceable (e.g., swap the PromQL-speaking TSDB without touching I4's contract;
  swap the collector for one that honors the I2 schema). This is the anti-lock-in posture made structural.
- **INV-8 — Components are defined by *role + conformance contract*; named products are default implementations.**
  C3 = "a metrics store satisfying I2 (scrape ingest) + I4 (vanilla-PromQL HTTP API)"; default impl = Prometheus.
  Optionality lives in the **contracts**, not in prematurely-flexible infra. Abstraction *machinery*
  (adapters/plugin layers) is built only when a second implementation actually exists — no speculative
  indirection (YAGNI). Coding to a standard API (e.g. the Prometheus HTTP API) already *is* coding to the contract.

---

## 5. Diagram

```
                            ┌──────────────── DATA PLANE (autonomous) ────────────────┐
                            │                                                          │
   C1 Junos devices  ──I1── │ ▶  C2 gNMIc  ──I2 (scrape /metrics)──▶  C3 Prometheus     │
        (gNMI/TLS)          │        ▲                                      ▲           │
                            └────────┼──────────────────────────────────────┼──────────┘
                                I3   │ desired target state          I4      │ PromQL (read-only)
                            ┌────────┴──────────────────────────────────────┴──────────┐
                            │                  C4  APPLICATION  (Go monolith)            │
   C7 Operators ──I6 (UI/API)──▶  inventory · collection-control · metrics-query ·       │
                            │      alarm-engine · dashboards · API · auth/RBAC           │
                            └─────┬───────────────────────────────┬─────────────────────┘
                            I8 │  │ (private)              I7 │    │  I5 (optional, pull)
                          C5 RDB ◀┘                  C8 notify ◀┘   ▶ C6 External SoT (NetBox/CSV/YAML)
```

---

## 6. Trust & failure boundaries

- **Sensitive seam:** device credentials at I3 (app → gNMIc). Store encrypted / via secret store; never plaintext in git.
- **TLS:** required on I1 (device↔gNMIc); I6 over HTTPS. I3/I4 typically within an internal trust zone.
- **Failure modes:** C4 down → data plane unaffected (INV-1); alarm evaluation pauses (mitigate via app HA or a
  minimal Prometheus-ruler backstop — see app-architecture forks). C2 down → collection stops (the real SPOF;
  gNMIc clustering is the later HA story). C3 down → no reads/alarms but gNMIc buffers nothing (scrape gap).

---

## 7. Component roles vs implementations

The architecture names **roles** (defined by the interfaces they satisfy). Products are *default
implementations*, swappable for anything honoring the same contract (INV-7, INV-8). Keep an interface only
where swapping is plausible; stay concrete elsewhere.

| Component | Role / conformance contract | Default impl | Plausible alternatives |
|-----------|-----------------------------|--------------|------------------------|
| **C3** | ingest via I2 (Prometheus scrape) **+** serve **vanilla-PromQL** over the Prometheus HTTP API v1 (I4) | single-node Prometheus | HA Prometheus pair; Prometheus + Thanos (querier/sidecar) |
| **C2** | a gNMI collector that emits the **I2 label schema** | gNMIc | (unlikely to swap; the *schema* is the contract, not the binary) |
| **C6** | `InventorySource` adapter feeding app inventory (I5) | native (none) | NetBox, CSV/YAML, others |
| **C8** | notifier accepting alarm raise/clear events (I7) | — | SMTP, Slack, webhook, PagerDuty |
| **C5** | app data store behind a private repository interface (I8) | SQLite | Postgres |

**Conformance caveat:** "PromQL-compatible" is leaky across systems (dialect/edge-case differences). The
portable contract is the **vanilla PromQL subset + Prometheus HTTP API v1** — keep queries within it so C3 stays
genuinely swappable (ties to the anti-lock-in posture). **Ingest mechanism (scrape vs remote_write) is a
*separate* decision from C3 pluggability** — don't conflate them.

---

## 8. Design rationale — three structural bets (vs LibreNMS)

This architecture is a reaction to where LibreNMS is hard to operate, extend, and contribute to. It makes
three deliberate bets. (Context: LibreNMS = poll-based SNMP monolith — discovery loop + polling loop + 35
poller modules + 231 OS classes + 1,496 YAML defs + 1,802 graph files + ~567 scattered SQL call sites + dual
frontends + dual APIs over one MySQL DB.)

**Bet 1 — Don't own the poller (data plane autonomy, INV-1).**
LibreNMS's largest and most-churned subsystem *is* the poller (+ queue manager + Python wrappers + distributed
polling). Here that role is **gNMIc + Prometheus** — mature, separately maintained, not our code. The app is a
control/read plane beside an autonomous data plane; collection survives the app being down.

**Bet 2 — OpenConfig-normalized device intelligence (the multi-vendor scaling bet).**
LibreNMS needs 231 OS classes + 1,496 YAML because **SNMP is unstandardized** — every vendor uses proprietary
MIBs, forcing a per-vendor OID translation table (irreducible for multi-vendor SNMP; the long-tail burden).
**OpenConfig inverts this**: a vendor-neutral model moves normalization onto the vendors. Consequences:
- A new OpenConfig-speaking OS ≈ "point gNMIc, subscribe the same paths" — near-zero device-intelligence cost.
- **Dashboards are vendor-agnostic** (normalized metrics + consistent labels, INV-3) → ~N reusable panels, not
  1,802 per-OS graph files.
- *Caveat:* OC coverage is uneven; native fallback (`/junos/...`) is vendor-specific again, so the long tail
  (PFE/optics/vendor features) still needs per-vendor mapping — but as **declarative, isolated** subscription/
  label profiles (data), not imperative code spread across layers.

**Bet 3 — Declarative inventory + gNMI auto-onboarding (not SNMP network-scan discovery).**
- *Which devices exist?* You point the platform at a device (host + gNMI creds); it **auto-detects** what the
  device is (gNMI `Capabilities` + a `/system`+`/components` `Get`), matches **detection-rule content** →
  vendor/model/role → a **default profile**, starts delivering value, and records it in inventory (the SoT,
  INV-2; onboarding *auto-populates* it). No subnet scanning to *find* devices, but no hand-crafting to get
  value either — instant-value, LibreNMS-style (see `extensibility.md`).
- *What's on each device?* gNMI **wildcard subscriptions** (`interface[name=*]`) stream whatever exists, keyed
  by name — no enumeration walk; new entities appear as new label values.
- Result: **no Poller and no continuous Discovery loop** — LibreNMS's biggest subsystem collapses into "M2
  configures gNMIc subscriptions; gNMIc + Prometheus do the rest." Auto-detection is a one-time/on-demand gNMI
  `Get`, never a scheduled SNMP-walk.

**Honesty clause.** Part of "easier to contribute to" is just being *young and narrowly scoped* (single-vendor,
metrics-only, standardized protocol) — not pure architecture. LibreNMS's complexity is partly *accidental*
(legacy accretion: dual frontends/APIs, dbFacile, jpgraph — which we avoid) and partly *essential* (the
problem domain — which we avoid by *narrowing scope*, not by being cleverer). The standing obligation is to
**keep accidental complexity low as scope grows** (clean module boundaries, one stack, declarative intelligence,
the minimal-interface heuristic in `c4-application.md` §1) — i.e., to not slowly *become* LibreNMS.
```
