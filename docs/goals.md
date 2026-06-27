# xenon — project goals & vision

**Status:** 2026-06-24

## What it is
An open-source network **monitoring platform** built on **gNMI / OpenConfig streaming telemetry** and the
Prometheus ecosystem — a modern, streaming-native answer to what LibreNMS does with SNMP. Built to be used by
**many different organizations and networks**, not one specific environment.

## Audiences & what each needs

**1. Operators (the platform's users / customers)**
- **Easy to run + instant value:** `docker compose up`, point it at a device with gNMI creds, and it
  auto-detects what the device is and starts delivering dashboards/alerts immediately (LibreNMS's best trait).
- **Customizable:** turn optional telemetry on/off (e.g. QoS), tune intervals/thresholds, add custom dashboards
  and alerts — **without forking**.
- **Options to scale:** a spectrum from a simple single-node install to a larger HA/scaled deployment, adopted
  *when needed*. Design the seams now; ship the simple default.

**2. Developers / maintainers (possibly one, plus PR contributors)**
- A **small, well-understood engine** that's easy to reason about and improve.
- **Clear extension points** so adding a vendor / profile / dashboard / alert is *content*, not core surgery.
- PR-friendly: an interested developer can contribute device support without touching the engine.

## Boundaries (the feature line)
- **gNMI / OpenConfig only** — the protocol boundary. No SNMP.
- **Metrics-focused** — no logs / flow / config-push (at least for now).
- **Vendor-agnostic engine; vendor support is content.** **Junos is the first vendor, not the only one** —
  expansion to other gNMI/OpenConfig-capable vendors is an explicit future goal.

## Core bets
- **OpenConfig normalization** makes dashboards/alerts/content portable across vendors (vs LibreNMS's per-OS
  graphs) — this is what makes "many networks + many vendors" tractable (architecture.md §8).
- **Engine vs content separation** — the engine stays small (developer story); device intelligence lives in
  bundled, community-contributable **content** + per-deployment **overlays** (operator + contributor story).
- **Two simplicities, two audiences:** simple to *run* (operators) and simple to *understand* (developers) are
  distinct goals; deployment *flexibility* (scale seams) lives at the external boundaries, not in the core.
- **Instant value via gNMI auto-detection** (capabilities + a system/components Get → identify → default
  profile + bundled dashboards), NOT SNMP-style network-scan discovery.

## Non-goals (for now)
SNMP; logs/syslog; flow (NetFlow/sFlow/IPFIX); config backup/push; non-gNMI protocols; being a topology/DCIM
source of truth (NetBox stays an *optional* integration).
