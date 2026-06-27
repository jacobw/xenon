# xenon vs LibreNMS — capability tally

**Status:** living reference · last updated 2026-06-22.
Tracks what xenon provides vs LibreNMS, against the *current design* (not yet built). Update as the
design/build evolves. Companion to `architecture.md` (§8 has the rationale) and `c4-application.md`.

**Legend**
| Tag | Meaning |
|-----|---------|
| **Parity** | Same capability, planned in the current design |
| **Better** | Improves on LibreNMS |
| **Different** | Covers the need a different way |
| **Planned** | Intended, not yet designed in detail |
| **Out** | Deliberately out of scope |
| **GAP** | LibreNMS provides it; we don't yet *and* it isn't clearly out-of-scope → **needs a decision** |

---

## Collection & protocols
| Capability | LibreNMS | xenon | Status |
|------------|----------|--------------|--------|
| Metric collection | SNMP v1/v2c/v3 polling | gNMI/OpenConfig **streaming** (dial-in) | Different |
| Collection model | scheduled poll (~5 min) | subscribe-once, push (sample/on_change) | Better |
| Protocol breadth | SNMP, Unix agent, IPMI, app monitoring | gNMI only | Out (by scope) |
| Resolution | poll-interval bound | per-path sample/on_change, sub-minute feasible | Better |

## Discovery & inventory
| Capability | LibreNMS | xenon | Status |
|------------|----------|--------------|--------|
| Device discovery | auto-scan (SNMP, CDP/LLDP/ARP/OSPF) | point-at-device + gNMI auto-detect → default profile (M2), recorded in inventory (M1) | Different |
| Entity discovery (ports/sensors) | scheduled discovery walk | gNMI wildcard subscriptions | Better |
| Inventory/management GUI | yes | yes (M1 + M4) | Planned |
| Hardware inventory (entPhysical) | yes (SNMP) | from OC `components` where modeled | Planned |
| External SoT integration | limited | pluggable adapter incl. NetBox (P2/I5) | Better |

## Metrics storage & query
| Capability | LibreNMS | xenon | Status |
|------------|----------|--------------|--------|
| TSDB | RRD (default), opt. InfluxDB/Prom/Graphite | Prometheus (role C3; Thanos/HA later) | Better |
| Query language | RRD/jpgraph internals | PromQL | Better |
| Long-term retention | RRA consolidation | local retention; Thanos if needed | Different |

## Visualization
| Capability | LibreNMS | xenon | Status |
|------------|----------|--------------|--------|
| Built-in graphs | 1,802 per-OS graph files | native panels (uPlot), **vendor-agnostic** | Better |
| Custom dashboards | yes | yes (M4/M6) | Planned |
| Ad-hoc exploration | limited | PromQL (+ optional Grafana adjunct) | Different |

## Alerting & fault
| Capability | LibreNMS | xenon | Status |
|------------|----------|--------------|--------|
| Rule-based alerting | yes | app-native engine (M3) | Planned |
| Alarm lifecycle (ack/clear/correlate/history) | yes | yes — first-class (M3) | Parity |
| Notification transports | many (email/Slack/PagerDuty/webhook/…) | pluggable notifiers (P3/I7) | Planned |
| Maintenance windows / scheduled mutes | yes | designed in M3 (window = time/recurrence + scope → suppress + mark in-maintenance) | Planned |
| Device up/down detection | ICMP + SNMP reachability | gNMIc session-state + collector `up` + (later) neighbor telemetry | Different |
| ICMP availability + RTT/loss | yes (fping) | deliberately omitted in base — optional add-on (see Decided note) | Out |
| SNMP trap handling | yes | — | Out |

## Logs & events
| Capability | LibreNMS | xenon | Status |
|------------|----------|--------------|--------|
| Syslog | yes (optional) | — | Out (metrics-only) |
| Eventlog / audit | yes | app audit log (Planned, M4) | Planned |

## Topology, maps & flow
| Capability | LibreNMS | xenon | Status |
|------------|----------|--------------|--------|
| Topology / availability maps | yes (LLDP/CDP) | — (could derive from OC LLDP later) | Out |
| Weathermaps | plugin | — | Out |
| NetFlow / sFlow / IPFIX | partial/plugins | — | Out |

## Config & automation
| Capability | LibreNMS | xenon | Status |
|------------|----------|--------------|--------|
| Config backup/diff | Oxidized integration | — (metrics-only; INV-6 no device-write) | Out |
| Service checks (Nagios-style) | yes | — | Out |
| Billing (bandwidth) | yes | — | Out |

## Access, API & platform
| Capability | LibreNMS | xenon | Status |
|------------|----------|--------------|--------|
| Users / RBAC | local, LDAP, AD, RADIUS, SSO/SAML, 2FA | local + OIDC (M4/M8) | Planned |
| Enterprise auth (LDAP/RADIUS) | yes | depends on environment | GAP |
| REST API | yes | yes (M4/I6) | Planned |
| Plugin system | yes | not planned (modular monolith) | Out |

## Multi-vendor & extensibility
| Capability | LibreNMS | xenon | Status |
|------------|----------|--------------|--------|
| Vendors supported | ~hundreds of OS | Junos only (today) | Out (now) |
| Cost to add a vendor | new OS class + YAML defs | mostly config if OpenConfig-covered | Better |
| Device-intelligence maintenance | 231 OS classes + 1,496 YAML | declarative subscription/label profiles | Better |
| Contributability | high coupling, hard | clean module boundaries | Better |

## Scale & operations
| Capability | LibreNMS | xenon | Status |
|------------|----------|--------------|--------|
| Footprint to run | PHP app + MySQL + RRD + poller fleet + Python wrappers | 1 Go binary + DB + gNMIc + Prometheus | Better |
| Distributed collection | distributed pollers | gNMIc clustering (later) | Different |
| HA | DB-backed, complex | deferred; app stateless-friendly | Out (now) |

---

## Net trade
- **xenon wins on:** streaming resolution, modern TSDB/PromQL, vendor-agnostic dashboards, cheap
  multi-vendor expansion (OpenConfig), operational simplicity, and contributability.
- **xenon is narrower:** single-vendor (now), metrics-only — so it drops SNMP, logs, traps, flow,
  topology, config backup, billing, service checks, and the plugin ecosystem **by design**.
- **Decided (2026-06-23) — no ICMP/blackbox in base.** ICMP-to-mgmt-IP is noisy (CoPP/rate-limiting) and tests
  the wrong layer; gNMIc **session-state** is a stronger mgmt-reachability signal, and **neighbor telemetry**
  reflects real data-plane reachability. Availability = gNMIc session-state + collector `up` + a single-vs-many
  heuristic (+ neighbor corroboration later). blackbox/ICMP stays an additive **optional add-on** — triggers:
  need an independent fault domain, onboarding-time reachability, or NOC ping expectations.
- **Open decisions to watch:**
  1. **Enterprise auth (LDAP/RADIUS)** — only if the environment needs it beyond local+OIDC (lands in M4).
- **Designed since:** maintenance windows → M3 (`m3-alarming.md`).
