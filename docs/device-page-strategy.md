# Device page strategy — LibreNMS parity on a gNMI/OpenConfig platform

Goal: grow the xenon device page toward LibreNMS completeness, but built on
OpenConfig streaming telemetry instead of per-vendor SNMP. This doc fixes *what to
collect* and *how to display it*, and the roadmap to get there.

## 1. Reference: the LibreNMS device page

Tabs under the hostname: **Overview · Graphs · Health · Ports · Routing · Map ·
Inventory · Logs · Alerts · Performance**.

- **Overview** — hardware, OS, uptime, location + headline graphs.
- **Health** — typed *sensors*: temperature, voltage, current, power, fanspeed,
  frequency, humidity, load, state, **dBm (optical)**, signal, charge, airflow…
- **Ports** — per-interface traffic (bps/pps), speed, errors, status; the port
  drill-down shows traffic/error/utilisation graphs **and optics** (DDM) inline.
- **Routing** — BGP/OSPF sessions, neighbours, prefixes.
- **Inventory** — hardware modules + serial numbers.

### The backbone idea — a sensor + 4-threshold model

LibreNMS's unifying mechanism is the **typed sensor with a threshold band**:
`low_limit` (crit-low) · `low_warn_limit` (warn-low) · `warn_limit` (warn-high) ·
`high_limit` (crit-high). That one band drives **both** the GUI colour (green /
amber / red) **and** alerting — and optical (dBm) sensors are simply attached to
their port.

xenon adopts the same model: a reading = `{class, value, unit, thresholds}` →
`status(ok|warn|crit)` → colours the panel **and** feeds the M3 alarm engine.
"How do I manage light levels?" is just this model applied to the `dBm` class, and
it generalises unchanged to temperature, fan, voltage, power, … Thresholds live in
**content** (bundled defaults per class/platform, operator-overridable), not code.

## 2. Mapping: LibreNMS section → OpenConfig gNMI → xenon

| Section | OpenConfig path | Junos sw1 | Tier | Where shown |
|---|---|---|---|---|
| System/Overview | `/system/state` (+ boot-time for uptime) | ✅ (uptime TBD) | base | Overview |
| CPU | `/system/cpus/cpu/state` | ✅ | base | Overview, Health |
| Memory | `/system/memory/state` | ✅ | base | Overview, Health |
| Temperature | `/components/component/state/temperature` | ✅ 43 °C | base | Health (`temperature`) |
| **Optics (dBm)** | `/components/component/transceiver/state/{input-power,output-power,laser-bias-current}` | ✅ **3 SFPs lit** | base (small) | Ports drill + Optics |
| Fans | `/components/component[type=FAN]/fan/state/speed` | TBD | base | Health (`fanspeed`) |
| Power supplies | `/components/component[type=POWER_SUPPLY]/power-supply/state/*` | TBD | base | Health (`power`/`voltage`/`current`) |
| Port traffic | `/interfaces/interface/state/counters/{in,out}-octets` | ✅ | base | Ports |
| Port errors/discards | `…/counters/{in,out}-errors,{in,out}-discards` | add | base | Ports + drill |
| Port status/speed | `/interfaces/interface/state/{oper,admin}-status`, `…/ethernet/state/port-speed` | partial | base | Ports |
| Routing (BGP) | `/network-instances/…/bgp/neighbors/neighbor/state/{session-state,…prefixes}` | sub exists, no GUI | base-if-present | Routing |
| Neighbours (LLDP) | `/lldp/interfaces/interface/neighbors/neighbor/state` | add | opt-in | Neighbours |
| Inventory | `/components/component/state/{type,serial-no,part-no,description,mfg-name,firmware-version}` | mostly | base (once/on-change) | Inventory |

Optics fact-found live on sw1: metrics are
`components_component_transceiver_state_{input_power,output_power,laser_bias_current}_instant`,
labelled `component_name="FPC0:PIC1:PORTx:Xcvr0"` (single-channel SFP; multi-lane
QSFP would add a `channel_index`).

## 3. Collection strategy (cardinality-aware — see `path-budget.md`)

- **Base path-groups** (every device): interfaces (curated counter leaves) +
  oper-status, cpu/mem, temperature, **optics**, inventory, BGP-if-present.
- **Opt-in path-groups** (scale with entities → cost): per-queue QoS, per-prefix
  RIB, full ethernet counters, LLDP.
- Budget by platform/device **scale**, not role. Optics is small (per-transceiver,
  3 leaves) → it belongs in base.

## 4. Display strategy

- Target tabs: **Overview · Ports · Optics · Health · Routing · Neighbours ·
  Inventory · Alarms · Config**.
- Every numeric reading is **drill-down clickable** (done) and **health-coloured**
  by its threshold band.
- Optics appears both inline on the **port drill-down** (LibreNMS "with the port")
  and as an **Optics** overview section.
- Health renders sensors grouped by class, each with current value, band, and
  sparkline.

## 5. The enum/state gap (grounded on sw1)

gnmic's Prometheus output **drops non-numeric values**, so OpenConfig **enum/string
leaves never become metrics**: `oper-status` (UP/DOWN), `admin-status`, ethernet
`port-speed` (`SPEED_1GB`), transceiver `form-factor`/`present` are all absent from
Prometheus. This blocks LibreNMS's port up/down dot and the `state` sensor class.

Fix (a dedicated pass): a gnmic processor that maps enums → a numeric `state` metric
carrying the string as a label (e.g. `…oper_status{state="UP"} 1`) — the standard
Prometheus state-set pattern. Numeric speed is available as
`interfaces_interface_state_high_speed` (Mbps) but lives outside the `counters`
container, and Junos streams whole containers, so it needs its own subscription.

## 6. Roadmap

1. ✅ **Optics (v0.1.8)** — Optics tab, `dBm` thresholds, low-Rx alarm. Proves the
   sensor/threshold backbone end to end.
2. ✅ **Port errors (v0.1.9)** — errors/discards column + port-drill error chart +
   interface-errors alarm. (No collection change — already in `counters`.)
3. ✅ **State/enum handling (v0.1.10–11)** — gnmic `strings-as-labels` emits enums as
   state-set metrics; Ports tab shows **UP/DOWN status** (down ports listed). Key
   rule: state leaves must be `sample` mode, not `on-change`, or they expire.
4. ✅ **Routing/BGP (v0.1.12–14)** — neighbours tab: neighbor·VRF·peer-AS·**state
   pill**·**prefixes rx/tx**·flaps, from `session_state` (state-set) + numeric
   peer-as/transitions + afi-safi prefix **counts**. Validated with a live gobgpd
   peer ([[gobgpd-bgp-test-harness]]). *Prefix counts only — actual prefixes (the
   RIB) are never stored in Prometheus (per-route cardinality; wrong tool).*
5. ✅ **Health — component status (v0.1.15)** — `/components/component/state` gives
   oper-status (PSU/cards/FRU: ACTIVE/DISABLED) + inventory. sw1 exposes **no fans /
   PSU-electrical** via OC, so component status + temperature is the hw-health view.
6. ✅ **Inventory (v0.1.16)** — hardware modules (part / serial / rev / description)
   from the component `/state` inventory leaves (strings-as-labels); 18 modules on
   sw1 incl PSU / cards / FRUs / transceivers.
7. **Link speed (DEFERRED — confirmed not viable in scrape model)** — widening the
   interface sub to full `/state` was tried (v0.1.16) and **reverted**: Junos
   suppresses *unchanged* leaves in the interface `/state` periodic samples, so
   idle-interface counters AND static `high_speed` stop being re-sent. (The
   "/state re-sends each sample" rule held for the smaller *component* `/state` sub,
   not interfaces.) Would need per-leaf heartbeat tuning or a different path.
8. **LLDP neighbours** — topology (the remaining LibreNMS device-page section).

Sources: [LibreNMS device page (NSRC lab)](https://nsrc.org/workshops/2016/rwnog-nmm/netmgmt/en/librenms/librenms-lab-1.htm),
[Health Information](https://docs.librenms.org/Developing/os/Health-Information/),
[Device Sensors](https://docs.librenms.org/Support/Device-Sensors/).
