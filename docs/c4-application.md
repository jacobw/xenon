# xenon — C4 Application: Internal Architecture

**Status:** draft · 2026-06-22 (rev 2 — *right-sized* after "too many concepts for a small team" feedback)
**Parent:** `docs/architecture.md` (this decomposes component **C4**).
**Scope:** internal **module decomposition**, structural level only. Each module's internal design is deferred.

**Style:** modular monolith. **Organize by feature module (cheap, helpful). Introduce an interface only where
a second implementation or a test genuinely needs it** — abstraction machinery is a cognitive cost, paid only
where it buys real swappability/testability. This is INV-8 applied to the app's *internals*. No event broker.

> Stable handles: modules `M#`, ports `P#`, app invariants `AINV-#`.
> (Rev 1 had 9 modules / 6 ports / an event bus — collapsed here; see §6.)

---

## 1. Guiding rule — minimize *concepts*, not just services

A small team must hold the whole app in its head. Feature boundaries help that; ports/adapters/buses hurt it
unless they pay their way.

> **Add an interface when (a) a 2nd real implementation exists or is committed, OR (b) a test must substitute
> the dependency. Otherwise use the concrete type. Add an event bus only for many-to-many events — we have none.**

---

## 2. Modules (5)

| ID | Module | Responsibility | Owns (in C5) |
|----|--------|----------------|--------------|
| **M1** | **Inventory** | Authoritative monitoring SoT (INV-2): **device records + monitoring config** (platform, credential ref, profile assignment + opt-ins, optional grouping tags, lifecycle state). **Device-centric — NOT sub-entities** (interfaces/sensors observed via telemetry, M2.d). Optional external-SoT sync (P2). Emits `inventory.changed`. See `m1-inventory.md`. | inventory |
| **M2** | **Telemetry** | The whole gNMIc↔Prometheus relationship in one place: compile inventory → gNMIc desired targets (control, via a concrete gNMIc client → I3) **and** the typed PromQL query API (observe, via P1 → I4). **Owns the device-identity label schema** as a private concern. Also **device auto-detection** (gNMI `Capabilities`/`Get` → detection-rule content → default profile; see `extensibility.md`). | — |
| **M3** | **Alarming** | App-native engine: rules (content) + eval loop (via M2.d) + alarm lifecycle (active/ack/cleared)/dedup/history + same-device correlation + **maintenance windows** + notification delivery (notifiers behind P3). See `m3-alarming.md`. | rules, alarm state/history, notify+window cfg |
| **M4** | **Presentation** | Inbound HTTP/API + HTMX/SSE UI; dashboards (panel specs, native charts via M2); authN + users/RBAC; serves the app's own `/metrics`. | users/roles/sessions, dashboard defs |
| **M5** | **Platform** | Config, logging, **scheduler** (background loops), DB plumbing, secrets/crypto, **content engine** (load bundled+overlay content, validate, merge per kind, registry — see `extensibility.md`), **composition root** (wires adapters → ports at startup). | — |

---

## 3. The three real ports — everything else is concrete

| Port | Realizes | Why it earns an interface | Default | Alternatives | In |
|------|----------|---------------------------|---------|--------------|----|
| **P1** `MetricsStore` | I4 | swap (Prometheus→Thanos) **and** test without a live Prometheus | Prometheus | Thanos / HA-pair | M2 |
| **P2** `InventorySource` | I5 | NetBox is **explicitly optional/pluggable** (a stated requirement) | native (none) | NetBox, CSV/YAML | M1 |
| **P3** `Notifier` | I7 | inherently several delivery channels | — | SMTP, Slack, webhook, PagerDuty | M3 |

**Used concretely (NOT ports — extract an interface later only if the heuristic in §1 triggers):**
- **gNMIc control** (I3) → a plain `gnmic` client package in M2. One collector, forever — no adapter ceremony.
- **Database** (I8) → direct data-access (e.g. sqlc-generated queries). At most *one* thin store interface if a
  test genuinely needs DB substitution — not a repository-per-module.
- **Auth/OIDC** (part of I6) → a library inside M4, not a port.

---

## 4. App invariants

- **AINV-1 — Dependency rule:** domain logic reaches external systems only through the 3 ports (or a thin
  internal client package); concrete adapters are wired at the M5 composition root.
- **AINV-2 — Single owner of device identity:** M2 (Telemetry) both tags gNMIc output and filters queries, so
  the device-identity label cannot drift. (Replaces rev-1's fragile three-module identity contract.)
- **AINV-3 — Background loops are singleton-per-cluster:** reconcile, alarm-eval, and SoT-sync run under M5's
  scheduler. Single instance today; **leader-elected** if app HA is ever added (no duplicate alarms on scale-out).
- **AINV-4 — Detection ≠ delivery, without a broker:** M3 calls a `Notifier` (P3) and an optional UI-pusher via
  a simple interface — direct calls, no event bus.
- **AINV-5 — Module data isolation:** each module owns its tables; modules collaborate via interfaces, never via
  another module's schema.
- **AINV-6 — Write-surface is tiny:** the app writes only (a) its own DB, (b) gNMIc target config (I3),
  (c) notifications (I7). Read-only to metrics (INV-4) and to devices (INV-6).

---

## 5. Diagram

```
 C7 Operators ─▶ M4 Presentation  (HTTP · REST/JSON · HTMX/SSE UI · authN · dashboards)
                       │ in-process service calls
        ┌──────────────┴───────────────────────────────────────────┐
        │  M1 Inventory        M2 Telemetry        M3 Alarming        │   ← M5 Platform
        └──────┬───────────────────┬───────────────────┬────────────┘     wires + schedules
            P2 │InventorySource  P1 │MetricsStore     P3 │Notifier
               ▼ I5                 ▼ I4                  ▼ I7
            C6 ext SoT           C3 Prometheus         C8 targets

   (concrete, no port:  M2 → gNMIc client → I3 → C2 gNMIc ;   M5 → DB → I8 → C5)
```

---

## 6. Representative flows

- **Add device:** C7 → M4 (authZ) → M1 create+persist → M2 reconcile → gNMIc client pushes target (I3) →
  gNMIc subscribes (I1) → metrics in C3 (I2) → M2 queryable → M4 shows it.
- **Alarm fires:** M5 scheduler tick → M3 evaluate via M2 (I4) → state→active → persist → M3 calls Notifier
  (P3/I7) **and** the UI-pusher → M4 streams SSE to the console.
- **SoT sync:** M5 scheduler → M1 pull via P2 (NetBox/I5) → reconcile inventory → M2 reconcile collection.

---

## 7. Deliberately NOT built (and the trigger to add it)

- **No event bus** → add only if alarm/inventory events become many-to-many.
- **No gNMIc port** → add only if a second collector is ever adopted.
- **No per-module repositories** → add a store interface only when a test needs DB substitution.
- **No standalone Identity/Dashboards/Notifications modules** → M2/M3/M4 may split back out *if and when* they
  grow unwieldy (Telemetry into control/observe; Alarming's notifications out). Split on real pressure, not upfront.
- **No ICMP/blackbox prober** → M3 derives availability from gNMIc **session-state** (telemetry/mgmt-plane up per
  device — a present-valued boolean, not absence) + collector **`up`** (collector-down) + a **single-vs-many**
  heuristic (one device's telemetry drops = that device; many at once = collector/segment). Neighbor-telemetry
  corroboration (X's peers see its links/BGP drop) is a later enhancement (needs adjacency data). Add blackbox/ICMP
  only if you need an independent fault domain, onboarding-time reachability, or NOC ping expectations.
```
