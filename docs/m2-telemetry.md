# xenon — M2 Telemetry: module design

**Status:** draft · 2026-06-23
**Parent:** `docs/c4-application.md` (decomposes module **M2**). Touches interfaces I2/I3/I4 and port P1 from
`docs/architecture.md`.
**Why one module:** both halves (push targets to gNMIc; query Prometheus) depend on the same device-identity
label, so keeping them together makes that the keystone contract a *private* concern (AINV-2), not a
cross-module agreement.

> Handles: parts `M2.a–M2.d`, module rules `M2-R#`.

---

## Internal structure (four parts, one module)

```
                         M2  TELEMETRY
   ┌─────────────────────────────────────────────────────────────┐
   │  M2.a  Identity & label schema   ← the contract (a)–(d) share │
   │  M2.b  Subscription profiles + compiler        (declarative)  │
   │  M2.c  Collector reconciler   ──I3 (HTTP-loader)──▶ gNMIc      │
   │  M2.d  Query API (typed)      ──P1/I4──▶ Prometheus           │
   └─────────────────────────────────────────────────────────────┘
```

---

## M2.a — Identity & label schema (keystone)

**Canonical metric identity = `device` label = hostname.** Human-readable everywhere (PromQL, Grafana, alerts);
the readability win is paid every day, the rename seam only rarely.

- **Normalization:** hostname stored/used in one canonical form — lowercased, and FQDN-vs-short chosen once and
  enforced. A domain migration would otherwise be an accidental mass-rename.
- **Uniqueness:** `device` (hostname) must be unique across the fleet — an inventory (M1) constraint.
- **Internal key ≠ label:** the app DB keys devices by a **surrogate ID** (PK); hostname is a unique attribute.
  So a rename is a one-row update and all references (alarms, dashboards, profile assignment) stay intact.
  M2 maps surrogate-ID ↔ hostname and owns the rename reconcile (M2.c drops the old gNMIc target, adds the new).
- **Accepted consequence:** a rename creates a Prometheus history seam (old series stops, new starts). No data
  loss; mitigable later via `label_replace` / an alias map if it ever matters.

**Label set on telemetry series (small rich set):**

| Label | Source | Notes |
|-------|--------|-------|
| `device` | hostname | the canonical join key; unique, normalized (required) |
| `platform` | auto-detected | vendor/model, e.g. `juniper` / `mx304` — drives profile selection |
| *(operator grouping tags)* | user-defined | OPTIONAL labels the operator applies for their own grouping/policy (e.g. `site`, `region`, `tenant`, or `role`). Not platform-defined, not required, **not a collection driver**. |
| *(OpenConfig-natural keys)* | the path | per-metric, e.g. interface `name`, BGP `neighbor-address`, component `name` |

- These are **constant-per-device** → they do **not** multiply series count; only a value *change* churns.
- **Volatile / rich metadata stays in inventory** (description, owner, serial, …) and is joined by the app at
  render time — not pushed into labels.
- **Label injection:** M2 supplies `device` + `platform` + any operator grouping tags per target when generating gNMIc config
  (target name = hostname + per-target tags). *Confirm exact gNMIc mechanism during build (target tags /
  `event-add-tag` processors / output add-labels).*
- **Metric naming:** derived from OpenConfig paths via gNMIc's prometheus-output conventions; M2 owns/documents
  the resulting names and applies processors to normalize and drop. Keep names stable and predictable.

---

## M2.b — Subscription profiles + compiler (declarative device intelligence)

- A **profile** = a named bundle of `{ OpenConfig path, mode (sample|on_change), interval, optional drop/keep }`,
  keyed by **platform/capability** — e.g. `juniper.mx304`, `juniper.qfx5120` — as a **universal OpenConfig base**
  + **platform native supplements** + **opt-in path-groups** (the expensive/bomb data). **Role does NOT drive
  collection:** feature presence self-prunes via gNMI (subscribe broad; absent features → no series), and bombs
  are opt-in per-path. This replaces LibreNMS's imperative OS classes: **data, not code.**
- **Config-as-code** (versioned YAML), loaded by M2. (DB-backed/UI-editable is a later option.)
- M1 inventory assigns each device a profile (by **auto-detected platform**, or explicit override).
- **Compiler:** `device × profile → concrete gNMI subscription set` + per-target labels + credential ref.
- **Cardinality budget lives here (M2-R3):** profile path-set × intervals = series/device. Curate paths +
  intervals and use gNMIc processors to drop unwanted paths to hold the budget (~target series/device).
- **OpenConfig-first**, with native (`/junos/...`) paths allowed in a profile only where OC doesn't model the
  data or behaves worse — isolated to that profile, not spread across the codebase.

---

## M2.c — Collector reconciler (control · I3 = gNMIc HTTP-loader, pull)

- **Mechanism:** M2 exposes an HTTP **`targets` endpoint** serving the gNMIc target configuration; gNMIc's
  **http loader** polls it on its own interval and re-syncs. Declarative, no shared filesystem, gNMIc owns its
  refresh cadence. (File-watch is the simple local fallback; REST only if imperative per-target ops are needed.)
- M2 is the **declarative source of desired target state**, computed from inventory + profiles. It keeps the
  endpoint current; `inventory.changed` triggers regeneration; gNMIc re-pulls and diffs (add/remove/update).
- **Target entry:** name = hostname · address = mgmt host/IP · credentials (resolved/ref via M5 secrets) ·
  subscriptions (from profile) · labels (`device`, `platform`, + operator grouping tags).
- **M2-R2 (sensitive seam):** the loader endpoint carries device credentials → it must be access-controlled
  (bind localhost / mTLS / authz). Credentials never in plaintext config or git (INV via M5 secrets).

---

## M2.d — Query API (observe · P1 → I4)

- Exposes **typed Go methods** so M3/M4/M6 never write raw PromQL; M2 builds the PromQL and encapsulates the
  label schema. Internally uses the `MetricsStore` port **P1** (Prometheus adapter, I4) — **read-only (INV-4)**.
- Examples: `InterfaceCounters(device, ifName, range)`, `DeviceHealth(device)`, generic `Query(spec)` for panels.
- **Availability/down signals** (per the 2026-06-23 decision) are surfaced here for M3:
  `TelemetryUp(device)` from gNMIc **session-state**, `CollectorUp()` from `up{job=gnmic}`, and the
  **single-vs-many** heuristic. (Neighbor-telemetry corroboration is a later enhancement needing adjacency data.)
- Enrichment is minimal (labels already carry `device` + `platform` + grouping tags); map hostname → inventory display only
  where needed.

---

## Module rules

- **M2-R1** — M2 is the **sole owner** of the device-identity label and the metric/label schema (AINV-2).
- **M2-R2** — the gNMIc loader endpoint carries credentials → access-controlled; secrets via M5, never in git.
- **M2-R3** — the **cardinality budget** is enforced in profiles + gNMIc processors (the #1 ops risk).
- **M2-R4** — gNMIc config is **generated, never hand-edited** (INV-5); M2 is the declarative source.
- **M2-R5** — telemetry labels are the small rich set; **volatile metadata stays in inventory**, joined at render.

---

## Deferred (not designed here)

- The concrete **OpenConfig path budget per profile** (the ~1,200-series curation) → its own doc.
- **Per-platform native-path supplements** (MX304 / QFX5120 / EX4100) → verify empirically (Juniper YDM
  Explorer, `gnmic capabilities`/`get`).
- **gNMIc processor specifics** (name normalization, drops) → detail at build time.
- **Neighbor-telemetry corroboration** for down-detection (needs adjacency data) → later.
