# xenon — GUI / Information Architecture

Status: **draft proposal** (2026-06-27). Frames the operator-facing UI for the
control plane (`cmd/xenond`). LibreNMS-inspired, reframed for gNMI/OpenConfig and
for xenon's engine-vs-content split. See `m1-inventory.md`, `m2-telemetry.md`,
`m3-alarming.md`, `extensibility.md`.

## 1. Framing principle — two planes, operator-first

The UI serves two audiences (the two kinds of simplicity from `goals.md`):

- **Monitoring plane (operator — the default 90% of the UI):** *is it healthy, is
  it performing, what's broken.* Up/down, traffic, sensors, alarms.
- **Configuration / engine plane (admin / integrator — demoted):** detection
  rules, profiles, content, onboarding, generated gNMIc config, cardinality.
  Reached via **Admin** and a per-device **Config** tab — never on monitoring views.

**Decision (this is the response to "I don't need to see onboarding status"):**
engine lifecycle / onboarding state is **not** something the operator sees on the
main views. It's a configuration-health signal, shown only where you manage config.

## 2. Device status model — two orthogonal statuses, don't conflate

- **Operational status (PRIMARY):** `Up` / `Unreachable` / `Degraded`, derived
  from the M2.d availability signals (gNMIc session-state + collector `up` +
  single-vs-many). This drives the status dot everywhere in the monitoring plane.
  (LibreNMS = up/down.)
- **Config status (secondary):** `Classified` / `Unclassified` / `Disabled` — the
  M1 engine lifecycle. A small "needs attention" badge in the Devices list and on
  the device Config tab; **never** the main status.

> Today the prototype shows only the *engine lifecycle* as the status pill
> (`active` / `unclassified`). The reframe flips this: operational status leads;
> lifecycle becomes a quiet config-health badge.

## 3. Top-level navigation

| Nav | Purpose | Primary source |
|---|---|---|
| **Overview** | Fleet dashboard: up/down + alarm counts, total traffic, fleet health graphs, top talkers, recent alarms | M-store + alarms + inventory |
| **Devices** | List: status, platform, site/role, uptime, traffic, alarm count. Search / filter / group | inventory + M-store |
| **Explore** | Multi-device / aggregate graphing: pick a metric, **group by a label** (site / role / platform / device) or compare chosen devices; presets for Traffic, Health/Sensors, Errors | M-store (PromQL) |
| **Alarms** | Active alarms + history; ack / clear (M3) | alarms |
| **Admin** ⚙ | Content (profiles, detection rules, alert packs), credentials, integrations (NetBox), **onboarding**, engine status | inventory + content + engine |

(Maps / topology, global search — later.)

### 3a. Two navigation axes — and the differentiator

Two ways in, deliberately:

- **Device-centric** (Devices → device → tabs/drill) — LibreNMS parity; good when
  you already know the device.
- **Metric / label-aggregate** (Explore + Overview) — pick a metric and group by a
  label, or compare chosen devices. This is the axis **LibreNMS effectively lacks**
  (device-by-device; RRD-per-device makes ad-hoc cross-device aggregation hard).
  xenon gets it for free: every metric carries the M2.a label set
  (`device`=hostname, `platform`, + operator tags `site`/`role`/…), and PromQL does
  `sum by (site)(rate(...))` / `avg by (platform)(...)` / `topk(...)` natively.
  **Treat this as a product differentiator with first-class placement (Explore),
  not a nice-to-have.**

**Why there is no top-level Ports:** a flat fleet-wide list of every interface is
mostly noise. The useful slices are *lenses* — top talkers, top errors — which are
Overview widgets / Explore `topk` queries; the full per-device port list lives on
the device **Ports** tab. "Ports" is a lens, not a section. (Same logic applies to
Health/Sensors: it's an Explore preset grouped by device/component, plus the device
**Health** tab — not a flat global dump.)

## 4. Device detail — tabs

- **Overview** — operational status + uptime; key health graphs (CPU / mem /
  temp); traffic summary; this device's active alarms; metadata (platform, site, role).
- **Ports** — interface table (status, in/out, errors, speed) + per-port graphs.
- **Health** — sensors: per-component temperatures, CPU, memory, power / PSU, fans.
- **Alarms** — this device's alarms.
- **Config** *(engine / admin)* — detection result + signature, assigned profile +
  opt-ins, subscriptions (incl. collector-side drops), planning cardinality, the
  generated gNMIc config. **Everything the prototype currently shows up front lives here.**

## 5. What moves — prototype → proposed

| Today | shows | → goes to |
|---|---|---|
| Devices list status pill | engine lifecycle (active / unclassified) | **operational** Up/Down; lifecycle → small badge |
| Device page (top of page) | detection, signature, est series, gNMIc config | **Config tab** (demoted) |
| Device page graphs / ports | live health + ports | become the **Overview / Ports / Health** tabs |
| Onboarding form | on the Devices list | **Admin → Onboard** (or a `+ Add` button → modal) |

## 6. Build status

- **Built:** Overview (partial), Devices list, device graphs + ports + drill-down, live onboarding.
- **Reframe needed:** flip the status model (operational vs lifecycle); demote engine
  internals into a **Config** tab; move onboarding to **Admin**; add device tabs.
- **Not built yet:** **Explore** (label group-by / compare — the differentiator),
  **Alarms** (M3), **Admin/content**. Health/Ports = Explore presets + device tabs,
  not standalone sections.

## 7. Cross-cutting

- **Status dot** component (Up/Unreachable/Degraded) reused on Overview, Devices, device header.
- **Graph** component (sparkline + click-to-drill, 1h/6h/24h/7d) reused on every view.
- Monitoring views read the **M-store** (Prometheus) and never the engine; the engine
  is reached only through Admin + Config — keeping the operator UI decoupled from
  engine internals (mirrors the app being off the metrics hot path).
