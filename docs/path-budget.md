# xenon — OpenConfig path budget

**Status:** draft · 2026-06-23 · **planning estimates — validate empirically**
**Parent:** `docs/m2-telemetry.md` (M2.b profiles). This puts concrete numbers behind the cardinality budget
(M2-R3) — the #1 operational risk.

> ⚠️ The instance counts (interfaces, optics, neighbors) and per-path leaf counts below are **planning
> estimates**. Confirm real counts per platform/release with `gnmic capabilities` / `gnmic get` and the Juniper
> YANG Data Model Explorer, then measure actual active series in Prometheus during a pilot. Treat this as the
> method + starting point, not ground truth.

---

## 1. Method

**series per path = (instances of the keyed element) × (leaves collected per instance).**
Sum across paths in a profile → series/device. Sum `series/device × device-count` across platforms/classes →
fleet active series. Keep fleet total under the single-node line (~8M; see architecture sizing).

**Cardinality is interval-independent.** The *budget* is series count. Sample interval is a *separate* dial that
affects ingest rate + disk, not series count. So tune the budget (paths/leaves/instances) and the intervals
independently.

---

## 2. Cardinality drivers & exclusions (the bombs)

| Driver | Rule |
|--------|------|
| **Interface counters** | The dominant consumer. **Curate the leaf set** (see §3) and **scope which interfaces** (physical + aggregate + routed units; skip irrelevant units). |
| **Per-queue × per-interface** (QoS) | The classic leaf/DC **bomb** (interfaces × ~8 queues × leaves). **Opt-in only** — uplinks/specific interfaces, not fleet-wide. |
| **Per-prefix / per-route** (RIB) | **Never.** Instant explosion. |
| **Per-BGP-neighbor** | State + aggregate prefix counts = fine. Per-neighbor *RIB/routes* = no. |
| **Optics/DOM** | Moderate but real on dense leaves — multiplies by transceiver × **lane**. Curate to rx/tx power + temp. |
| **Subinterfaces/units** | A silent multiplier on cores. Collect counters only where you need them. |
| **Per-process / per-MAC / per-LSP** | Opt-in, scoped — never blanket. |

---

## 3. Base path group (all profiles)

`N_if` = interface instances collected. Interface counters curated to **~10 leaves** (in/out octets, in/out
unicast/mcast/bcast pkts, in/out errors, in/out discards — dropping fcs-errors/unknown-protos/last-clear).

| Path | Mode | Interval | Instances | Leaves | Series |
|------|------|----------|-----------|--------|--------|
| `…/interfaces/interface/state/counters` | sample | 30–60s | `N_if` | ~10 | `10·N_if` |
| `…/interfaces/interface/state` (oper/admin/last-change) | on_change | — | `N_if` | ~3 | `3·N_if` |
| `…/system/cpus/cpu/state` | sample | 60s | ~4 | ~3 | ~12 |
| `…/system/memory/state` | sample | 60s | 1 | ~3 | ~3 |
| `…/components` env (temp/fan/power) | sample | 60s | ~30 | ~2 | ~60 |
| `…/components/…/transceiver` + `optical-channel` (DOM) | sample | 60s | optics×lanes | ~3 | `3·(optics·lanes)` |

So the base ≈ **`13·N_if` + ~80 + optics**, and **interfaces dominate everything**.

---

## 4. Starter profiles (planning estimates)

Profiles are keyed by **platform**, not role. The "core / leaf / access" labels below are just *typical
deployment scales* for illustration — the real series count comes from each device's **actual** interface /
neighbor / optic counts and which features it runs (measure it, §8), not from a declared role.

### `juniper.mx304` — typical core (many interfaces + BGP)
| Group | Assumptions | Series |
|-------|-------------|--------|
| Interfaces | `N_if ≈ 100` (physical + key units + ae) | ~1,300 |
| Optics | ~30 transceiver-lanes × 3 | ~90 |
| System + env | | ~90 |
| BGP neighbors | ~40 nbrs × ~10 (state on_change + prefix counts) | ~400 |
| **Subtotal** | | **≈ 1,900–2,100** |

### `juniper.qfx5120` — typical EVPN leaf
| Group | Assumptions | Series |
|-------|-------------|--------|
| Interfaces | `N_if ≈ 60` | ~780 |
| Optics | ~88 lanes (48×25G + 8×100G@4 lanes) × 3 | ~260 |
| BGP (underlay+overlay) | ~15 × 10 | ~150 |
| System + env | | ~90 |
| **Subtotal (no queues)** | | **≈ 1,280** |
| *Queues (if added)* | *60 if × 8 q × 2 = **bomb*** | *+960 → opt-in only* |

### `juniper.ex4100` — typical access switch
| Group | Assumptions | Series |
|-------|-------------|--------|
| Interfaces | `N_if ≈ 48` | ~624 |
| PoE | 48 ports × 2 | ~96 |
| Optics (uplinks) | ~4 × 5 | ~20 |
| LLDP (topology, on_change) | ~48 × 2 | ~96 |
| System + env | | ~50 |
| **Subtotal** | | **≈ 890** |

---

## 5. The headline findings

- **Interfaces dominate.** ~13 series per interface means interface paths alone consume most of the budget.
  The biggest levers are **which counter leaves** (curate to ~10) and **which interfaces/units** you collect.
- **~1,200 is not a flat number — it tracks device scale, not a declared role.** A big router (many interfaces
  + BGP) lands ~2,000; a small access switch ~900. Budget by **measuring actual series per platform/device**,
  not by a role label.
- **Queues are the leaf bomb** — fleet-wide per-queue stats nearly double a leaf's series. Keep opt-in.
- **Optics matter more than expected on dense leaves** — transceiver × lane adds up; curate the DOM leaf set.

**Fleet roll-up:** `Σ (series/role × devices/role)` must stay under ~8M. Example: 50 cores×2,000 + 500 leaves×1,300
+ 4,000 access×900 = 0.1M + 0.65M + 3.6M ≈ **4.4M** → comfortable single-node. Recompute with your real mix.

---

## 6. Interval guidance (the separate cost dial)

| Signal | Mode / interval |
|--------|-----------------|
| Interface counters (octets/pkts) | sample 30–60s |
| Oper/admin state, BGP session, LACP | **on_change** |
| Optics/DOM | sample 60s |
| System CPU/mem | sample 60s |
| Environment (temp/fan/power) | sample 60s (or on_change for discrete state) |

Faster intervals raise ingest/disk, **not** series count.

---

## 7. Native-path supplements (where OpenConfig falls short)

OpenConfig-first; add native `/junos/...` paths to a profile only where OC doesn't model the data or behaves
worse. Known candidates (verify per platform/release):
- **QFX/EX:** PFE / Broadcom counters (buffer occupancy, ASIC drops), some EVPN/VXLAN detail.
- **MX:** finer class-of-service, MPLS/LSP detail.
- Native paths are **isolated to the profile that needs them** (M2.b) — they don't spread across the codebase.

---

## 8. Validation method (before trusting any number above)

1. `gnmic capabilities` per platform/release → what models/paths are supported.
2. `gnmic get` a representative path on one of each (MX304 / QFX5120 / EX4100) → real leaf coverage + instance counts.
3. Pilot one device per profile → **measure actual active series in Prometheus** (`count({device="…"})`).
4. Adjust profiles to hit the per-platform/device budget; re-roll-up the fleet total.
5. Confirm the collector **service account has read permission** for every subscribed path (a path can be
   permitted to subscribe yet return permission-denied) — vendor/AAA-specific, a deployment-time check.
