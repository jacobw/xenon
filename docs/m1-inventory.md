# xenon — M1 Inventory: module design

**Status:** draft · 2026-06-25
**Parent:** `docs/c4-application.md` (decomposes module **M1**). Touches I5 (port P2 `InventorySource`); feeds M2
(compile targets); consumed by M3/M4.

M1 is the authoritative source of truth for **what the platform monitors and how** — the record M2 compiles
into gNMIc targets and M3/M4 read. **Device-centric and deliberately small.**

> Handles: module rules `M1-R#`.

---

## What M1 owns — and what it doesn't

**Owns:** device records + their *monitoring config* (platform, credential ref, profile assignment + opt-ins,
optional grouping tags, lifecycle state); credential sets (references); optional external-SoT links.

**Does NOT own — deliberately:**
- **Sub-device entities (interfaces, sensors, components).** These are **observed via telemetry** (queried
  through M2.d), **never enumerated or stored** (M1-R4). Storing them re-introduces the discovery/staleness
  problem we deliberately avoided. "Device has 48 interfaces, 2 down" is a *query*, not a table.
- **Profile *definitions*** — M2 owns those (bundled content); M1 owns the *assignment* (device → profile +
  opt-ins).
- **Metrics** — those live in Prometheus.

---

## Core data model (lean)

**`device`** — the central entity:
- `id` — surrogate PK; stable identity (rename / re-IP / re-platform are **attribute updates**, not identity
  changes — M1-R3).
- `hostname` — unique, normalized; becomes the `device` metric label (M1-R2; the M2.a contract).
- `mgmt_address` — host/IP for gNMI.
- `platform` — `{vendor, model, os, version}` — auto-detected (via M2) or set explicitly.
- `credential_ref` → `credential_set`.
- `profile` — assigned platform profile (auto-default or explicit) + per-device opt-ins/overrides
  (e.g. QoS on, interval override).
- `tags` — optional operator key/value grouping (`site`, `role`, `tenant`, …); operator designates which are
  **promoted to metric labels** (the M2.a optional grouping labels).
- `state` — lifecycle (below).
- `source` + detection provenance — manual / SoT / auto-detected.
- timestamps.

**`credential_set`**: `id`, `name`, `auth_type` (user-pass / client-cert), `username`, `secret_ref` → **M5
secret store**. **No secret value stored** (M1-R5). Shared across devices (one service account → many devices).

**`group`** *(optional)*: a named tag-selector or explicit membership — for targeting (dashboards, alert
routing, access scoping, group-level profile opt-ins). Can start as just tag selectors.

> *(tags / profile-overlay / group can begin as JSON columns / simple tables on `device`; split out only if
> needed — keep the model lean.)*

---

## Lifecycle states

`pending` (added) → `detecting` → **`active`** (profiled, collecting) · `unclassified` (fell back to the
generic profile — surfaced for attention) · `unreachable` (can't connect/auth) · `disabled` (operator) ·
`decommissioned`. M2 acts on `active`; **misclassification / unreachable are visible, never silent.**

---

## Onboarding interaction (M1 ↔ M2)

1. Device added (manual or SoT sync) → `pending`.
2. M1 asks M2 to **detect** (gNMI `Capabilities` + `/system` + `/components`).
3. Detection rules → platform → default profile; M1 records platform + assignment → `active`
   (or `unclassified` / `unreachable`).
4. M1 emits `inventory.changed`; M2 compiles gNMIc targets; collection starts; bundled dashboards/alerts light
   up → instant value.

---

## External SoT sync (optional · P2 / I5)

- Optional `InventorySource` adapter (NetBox / CSV / YAML) pulls upstream → reconciles into M1.
- **M1 stays authoritative for monitoring config** (profile / opt-ins / state); the SoT feeds *existence +
  attributes* (hostname, mgmt IP, platform hints, tags). Layered like content: SoT-sourced fields vs local
  edits — **local edits preserved / take precedence** (M1-R1).
- One-way (upstream → M1) by default. **A device removed from the SoT → soft-disabled + flagged, never
  auto-deleted** (M1-R7) — preserve history; decommission is explicit.

---

## Module rules

- **M1-R1** — authoritative SoT for *monitoring config*; external SoT only feeds existence/attributes (INV-2).
- **M1-R2** — hostname unique + normalized (the `device`-label contract with M2.a).
- **M1-R3** — surrogate ID is identity; hostname / IP / platform are mutable attributes.
- **M1-R4** — **device-centric: no stored sub-entities** (interfaces/sensors observed via telemetry, M2.d).
- **M1-R5** — credentials are references; secret values live in M5, never in inventory rows or exports.
- **M1-R6** — changes emit events (`inventory.changed`) → M2 reconciles; M1 is off the data hot path (INV-1).
- **M1-R7** — SoT-removed devices soft-disabled, not auto-deleted.

---

## Consumed by

- **M2** — `active` devices → address + credential_ref + assigned profile + opt-ins + label-tags → compile gNMIc targets.
- **M3** — device list + tags for alert scoping/routing.
- **M4** — device list / state / tags for the UI; add / edit / tag / assign / enable via API.

---

## Deferred

- **Multi-tenancy** (many orgs in ONE deployment) — out of scope now; default is one deployment per org. The
  tag + RBAC model doesn't preclude adding it later.
- Detailed **profile-overlay / group schema** — shared with the content-overlay model (`extensibility.md`).
- SoT **field-level conflict/precedence** details.
- **Bulk import / onboarding UX.**
