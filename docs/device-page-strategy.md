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

## 5. Roadmap

1. **Optics (now)** — collect + Optics section + `dBm` thresholds + low-Rx alarm
   rule. Proves the sensor/threshold backbone end to end.
2. **Port detail** — errors/discards + status + speed; port drill gains an error
   graph; ports list gains status + speed columns.
3. **Health sensors** — fans / PSU / voltage / power (discover Junos coverage) and
   generalise the sensor+threshold framework across all classes.
4. **Routing** — BGP neighbours tab (session state, accepted/advertised prefixes).
5. **Neighbours + Inventory** — LLDP topology, hardware/serial tree.

Sources: [LibreNMS device page (NSRC lab)](https://nsrc.org/workshops/2016/rwnog-nmm/netmgmt/en/librenms/librenms-lab-1.htm),
[Health Information](https://docs.librenms.org/Developing/os/Health-Information/),
[Device Sensors](https://docs.librenms.org/Support/Device-Sensors/).
