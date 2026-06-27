# xenon — extensibility & content model

**Status:** draft · 2026-06-24
**Parent:** `docs/goals.md`, `docs/c4-application.md`. Realizes the engine-vs-content bet.

## Principle: engine vs content
The Go app (M1–M5) is the **engine** — small, vendor-agnostic, the thing a maintainer reasons about.
**Content** is everything that knows about specific devices/vendors and how to present them. Adding device
support or letting an operator customize is **content**, not engine surgery. This single split serves all three
goals: operator customization, instant value, and the maintainer/contributor story.

## Content is an OPEN, typed, layered, mergeable set
A **content kind** is a typed, versioned, mergeable unit of platform behavior. **The set of kinds is open** —
the table below lists the *currently known* kinds, not the definition. A new kind is added by registering a
schema + a merge rule; the engine does not change. (Examples like "QoS toggle / custom graph / custom alert"
are just instances of this — don't hard-code around them.)

Every kind shares the same framework:
- **Typed schema** — defined fields, validated on load/save.
- **Stable identity** — a key (e.g. `juniper.mx.core`).
- **Two layers** — **bundled** (ships with releases, community-PR'd) + **overlay** (per-deployment, in the DB,
  UI/API-editable, **survives upgrades**).
- **One format, both layers** — an overlay item can be *exported* and contributed back as bundled content (a
  frictionless "I built something useful → PR it" path).
- **Per-kind merge** — the kind defines how overlay composes with bundled (whole-object replace, or sparse
  field/sub-element override for richer kinds like profiles).
- **Validation against the metric/label schema (M2.a)** — content referencing unknown paths/metrics/labels fails
  **loudly at load/save**, not silently at render/eval. This is what keeps a customizable platform from rotting
  into broken user content.
- **Provenance + upgrade-safe** — overlays stored as sparse overrides referencing the bundled identity where
  possible, so bundled improvements flow through unless the user explicitly overrode that field; conflicts
  surface to the user rather than silently breaking.

## Known content kinds (illustrative, not exhaustive)
| Kind | What | Owning module | Powers |
|------|------|---------------|--------|
| Device profiles | subscription path-bundles by vendor/platform/**capability** (NOT role); base + native supplements + opt-in path groups (e.g. QoS) | M2 | what's collected; tuning |
| Detection rules | gNMI capabilities + `/system`+`/components` signature → **vendor/model** → default platform profile | M2 | auto-onboarding |
| Dashboards / panels | PromQL panel specs over the normalized schema | M4 | visualization; custom graphs |
| Alert rules / packs | PromQL expr + threshold + severity + lifecycle metadata | M3 | alerting; custom alerts |
| *…future kinds…* | e.g. **operator grouping/tags** (site/region/role — a user label, NOT a collection driver), notification templates, SLO/threshold libraries, enrichment/metadata maps, report templates | various | open-ended — add by registration |

## Where the framework lives
- **Mechanism in M5 (Platform):** a generic content engine — load bundled + overlay layers, validate, merge
  per kind, expose the **effective** set via a registry. One mechanism, reused — not per-module reinvention.
- **Schemas owned by the consuming modules (AINV-5):** M2 owns profile + detection-rule schemas; M3 the
  alert-rule schema; M4 the dashboard schema. Each *registers* its kind with M5's engine; overlay rows are owned
  by the consuming module's tables.
- Modules ask for **effective** content ("the effective profile for device X") — they never see the layers.

## Auto-onboarding (the instant-value flow)
1. `docker compose up`; operator adds a device (host + gNMI creds).
2. **M2 auto-detects:** gNMI `Capabilities` + a `Get` of `/system` + `/components`.
3. **Detection rules** (content) match the signature → **vendor/model** → **default platform profile** (no role needed — most value is role-independent; bombs/extras are opt-in).
4. **M1** records the device + auto-populated attributes + profile assignment (inventory stays the SoT, INV-2;
   onboarding *auto-populates* it instead of demanding hand-entry).
5. Subscriptions start; **bundled dashboards/alerts** keyed on the normalized schema light up → **instant value**.
6. Operator customizes via overlays (toggle QoS, tweak intervals/thresholds, add dashboards/alerts) — no fork.

This **revises the earlier "no discovery" stance**: still **no SNMP network-scan to find devices**, but **gNMI
auto-detection at onboarding** for instant value — a one-time / refresh-on-demand `Get`, not a continuous loop.

## Multi-vendor = a content bundle
A new vendor is mostly **content**: profiles + detection rules + dashboards (+ alert packs), plus — only where
the vendor's native models need normalization — a small, isolated native-path adapter (code). The engine stays
vendor-agnostic and unchanged. Enabled by OpenConfig normalization (architecture.md §8, Bet 2). This is the
contributor + expansion story.

## Decisions & deferred
- **Bundled content location (decided):** in-repo, compiled into the binary now (simplest deploy, versioned with
  releases); a decoupled content directory/registry is a later option (INV-8: add when a real need appears).
- **Deferred:** per-kind merge / 3-way-override details; content versioning + migration on upgrade; export→PR
  tooling; the detection-rule schema. Each is detailed in its owning-module doc.
